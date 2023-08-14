FROM golang:1.21

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

RUN go build -o /app/build

COPY . .

ENV PORT=8080

EXPOSE 8080

CMD ["app/build"]