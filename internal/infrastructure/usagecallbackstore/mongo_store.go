package usagecallbackstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	mongoclient "github.com/poly-workshop/go-webmods/mongoclient"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoConfig struct {
	URI        string
	Database   string
	Collection string
}

type mongoStore struct {
	client     *mongo.Client
	dbName     string
	collection string
}

type mongoDoc struct {
	ID        string    `bson:"_id"`
	URLs      []string  `bson:"urls"`
	UpdatedAt time.Time `bson:"updated_at"`
	CreatedAt time.Time `bson:"created_at"`
}

func NewMongo(cfg MongoConfig) (auth.UsageCallbackStore, error) {
	if strings.TrimSpace(cfg.URI) == "" {
		return nil, fmt.Errorf("missing mongodb uri")
	}
	if strings.TrimSpace(cfg.Database) == "" {
		return nil, fmt.Errorf("missing mongodb database")
	}
	coll := strings.TrimSpace(cfg.Collection)
	if coll == "" {
		coll = "llm_gateway_usage_callback_allowlists"
	}
	client := mongoclient.NewClient(mongoclient.Config{
		URI:      cfg.URI,
		Database: cfg.Database,
	})
	return &mongoStore{
		client:     client,
		dbName:     cfg.Database,
		collection: coll,
	}, nil
}

func (s *mongoStore) coll() *mongo.Collection {
	return s.client.Database(s.dbName).Collection(s.collection)
}

func (s *mongoStore) GetAllowlist(ctx context.Context, subject string) ([]string, error) {
	if subject == "" {
		return nil, nil
	}
	var out mongoDoc
	err := s.coll().FindOne(ctx, bson.M{"_id": subject}).Decode(&out)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	if len(out.URLs) == 0 {
		return nil, nil
	}
	return out.URLs, nil
}

func (s *mongoStore) SetAllowlist(ctx context.Context, subject string, urls []string) error {
	if subject == "" {
		return nil
	}
	if len(urls) == 0 {
		_, err := s.coll().DeleteOne(ctx, bson.M{"_id": subject})
		return err
	}
	now := time.Now()
	_, err := s.coll().UpdateOne(
		ctx,
		bson.M{"_id": subject},
		bson.M{
			"$set": bson.M{
				"urls":       urls,
				"updated_at": now,
			},
			"$setOnInsert": bson.M{
				"created_at": now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (s *mongoStore) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}
