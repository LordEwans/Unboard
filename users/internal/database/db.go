package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bottlehub/unboard/users/configs"
	"github.com/bottlehub/unboard/users/graph/model"
	"github.com/bottlehub/unboard/users/internal"
	"github.com/bottlehub/unboard/users/internal/mq"
	"github.com/machinebox/graphql"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type DB struct {
	client *mongo.Client
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func ConnectDB() (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(configs.EnvMongoURI()))
	if err != nil {
		log.Fatal(err)
	}

	if err != nil {
		log.Fatal(err)
	}

	//ping the database
	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Connected to MongoDB!")
	return &DB{client: client}, err
}

func colHelper(db *DB, collectionName string) *mongo.Collection {
	return db.client.Database("UserBase").Collection(collectionName)
}

func (db *DB) ctxDeferHelper(collectionName string) (*mongo.Collection, context.Context) {
	collection := colHelper(db, collectionName)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	fmt.Println(cancel)

	return collection, ctx
}

func (db *DB) resErrHelper(collectionName string, input any) (*mongo.InsertOneResult, error) {
	collection, ctx := db.ctxDeferHelper(collectionName)

	res, err := collection.InsertOne(ctx, input)

	if err != nil {
		log.Fatal(err)
	}

	return res, err
}

func (db *DB) multipleFetchHelper(collectionName string, ID string) (*mongo.Cursor, context.Context) {
	collection, ctx := db.ctxDeferHelper(collectionName)

	objId, _ := primitive.ObjectIDFromHex(ID)

	res, err := collection.Find(ctx, bson.M{ID: objId})
	if err != nil {
		log.Fatal(err)
	}

	return res, ctx
}

func (db *DB) deleteHelper(collectionName string, ID string) error {
	collection, ctx := db.ctxDeferHelper(collectionName)

	objId, _ := primitive.ObjectIDFromHex(ID)
	filter := bson.M{"_id": objId}

	_, err := collection.DeleteOne(ctx, filter)
	if err != nil {
		log.Panic(err)
	}

	return err
}

func (db *DB) updateHelper(collectionName, ID string, info bson.M) *mongo.SingleResult {
	collection, ctx := db.ctxDeferHelper(collectionName)
	_id, _ := primitive.ObjectIDFromHex(ID)
	filter := bson.M{"_id": _id}
	update := bson.M{"$set": info}

	results := collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(1))

	return results
}

func (db *DB) CreateUser(input *model.NewUser) (*model.User, error) {
	res, err := db.resErrHelper("users", input)

	user := &model.User{
		ID: res.InsertedID.(primitive.ObjectID).Hex(),
	}

	return user, err
}

func (db *DB) GetUsers(ID string) ([]*model.User, error) {
	res, ctx := db.multipleFetchHelper("users", ID)
	var (
		users []*model.User
		err   error
	)

	defer res.Close(ctx)

	if err = res.All(context.TODO(), users); err != nil {
		panic(err)
	}

	return users, err
}

func (db *DB) SingleUser(ID string) (*model.User, error) {
	collection, ctx := db.ctxDeferHelper("users")
	var user *model.User

	objId, _ := primitive.ObjectIDFromHex(ID)

	err := collection.FindOne(ctx, bson.M{"_id": objId}).Decode(&user)

	return user, err
}

func (db *DB) UpdateUser(ID string, input *model.UpdateUser) (*model.User, error) {
	var user *model.User

	updateInfo := bson.M{}

	if input.About != nil {
		updateInfo["about"] = input.About
	}
	if input.AvatarImageURL != nil {
		updateInfo["avatarImageURL"] = input.AvatarImageURL
	}
	if input.Name != nil {
		updateInfo["name"] = input.Name
	}
	if input.Following != nil {
		user1, err := db.SingleUser(ID)
		if err != nil {
			log.Fatal(err)
		}
		user2, err := db.SingleUser(*input.Following)
		if err != nil {
			log.Fatal(err)
		}

		arr := user1.Following
		arr = append(arr, user2)
		updateInfo["following"] = arr
	}
	if input.Follower != nil {
		user1, err := db.SingleUser(ID)
		if err != nil {
			log.Fatal(err)
		}
		user2, err := db.SingleUser(*input.Follower)
		if err != nil {
			log.Fatal(err)
		}

		arr := user1.Followers
		arr = append(arr, user2)
		updateInfo["followers"] = arr
	}
	if input.ChatBoard != nil {
		client := graphql.NewClient("https://localhost:8090/graphql")
		query := fmt.Sprintf(`
		{
			input(ID: %s) {
				id
				name
				imageURL
				description
				members
				messages
			}
		}
		`, *input.ChatBoard)

		user1, err := db.SingleUser(ID)
		if err != nil {
			log.Fatal(err)
		}
		var chatboard *model.Chatboard
		request := graphql.NewRequest(query)
		if err := client.Run(context.Background(), request, &chatboard); err != nil {
			internal.Handle(err)
		}

		arr := user1.ChatBoards
		arr = append(arr, chatboard)
		updateInfo["chatboards"] = arr
		mq.Publish("update-chatboard", fmt.Sprintf("updatedID: %s, updatingID: %s", *input.ChatBoard, ID))
	}

	results := db.updateHelper("users", ID, updateInfo)
	if err := results.Decode(&user); err != nil {
		log.Fatal(err)
		return user, err
	}

	return user, nil
}

func (db *DB) DeleteUser(ID string) (*model.DeleteUser, error) {
	err := db.deleteHelper("users", ID)
	var delete *model.DeleteUser

	return delete, err
}
