package llmconfigstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	mongoclient "github.com/poly-workshop/go-webmods/mongoclient"
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
	client    *mongo.Client
	dbName    string
	provColl  string
	modelColl string
}

type providerDoc struct {
	ID             string    `bson:"_id"`
	BaseURL        string    `bson:"base_url"`
	APIKey         string    `bson:"api_key"`
	TimeoutSeconds int64     `bson:"timeout_seconds"`
	UpdatedAt      time.Time `bson:"updated_at"`
	CreatedAt      time.Time `bson:"created_at"`
}

type modelDoc struct {
	ID              string    `bson:"_id"`
	Name            string    `bson:"name"`
	Provider        string    `bson:"provider"`
	Capabilities    []string  `bson:"capabilities"`
	UpstreamModel   string    `bson:"upstream_model"`
	MaxOutputTokens uint32    `bson:"max_output_tokens"`
	UpdatedAt       time.Time `bson:"updated_at"`
	CreatedAt       time.Time `bson:"created_at"`
}

func NewMongo(cfg MongoConfig) (Store, error) {
	if strings.TrimSpace(cfg.URI) == "" {
		return nil, fmt.Errorf("missing mongodb uri")
	}
	if strings.TrimSpace(cfg.Database) == "" {
		return nil, fmt.Errorf("missing mongodb database")
	}
	base := strings.TrimSpace(cfg.Collection)
	if base == "" {
		base = "llm_gateway"
	}
	client := mongoclient.NewClient(mongoclient.Config{
		URI:      cfg.URI,
		Database: cfg.Database,
	})
	return &mongoStore{
		client:    client,
		dbName:    cfg.Database,
		provColl:  base + "_provider_configs",
		modelColl: base + "_models",
	}, nil
}

func (s *mongoStore) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Disconnect(ctx)
}

func (s *mongoStore) prov() *mongo.Collection {
	return s.client.Database(s.dbName).Collection(s.provColl)
}
func (s *mongoStore) models() *mongo.Collection {
	return s.client.Database(s.dbName).Collection(s.modelColl)
}

func (s *mongoStore) UpsertProviderConfig(ctx context.Context, cfg ProviderConfig) error {
	if cfg.Provider == "" {
		return nil
	}
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"base_url":        cfg.BaseURL,
			"api_key":         cfg.APIKey,
			"timeout_seconds": normalizeTimeoutSeconds(cfg.TimeoutSeconds),
			"updated_at":      now,
		},
		"$setOnInsert": bson.M{
			"created_at": now,
		},
	}
	_, err := s.prov().UpdateOne(ctx, bson.M{"_id": cfg.Provider}, update, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *mongoStore) DeleteProviderConfig(ctx context.Context, provider string) error {
	if provider == "" {
		return nil
	}
	_, err := s.prov().DeleteOne(ctx, bson.M{"_id": provider})
	return err
}

func (s *mongoStore) ListProviderConfigs(ctx context.Context) ([]ProviderConfigView, error) {
	cur, err := s.prov().Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []ProviderConfigView{}
	for cur.Next(ctx) {
		var d providerDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, ProviderConfigView{
			Provider:       d.ID,
			BaseURL:        d.BaseURL,
			TimeoutSeconds: d.TimeoutSeconds,
			APIKeyPresent:  d.APIKey != "",
		})
	}
	return out, cur.Err()
}

func (s *mongoStore) GetProviderConfigs(ctx context.Context) ([]ProviderConfig, error) {
	cur, err := s.prov().Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []ProviderConfig{}
	for cur.Next(ctx) {
		var d providerDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, ProviderConfig{
			Provider:       d.ID,
			BaseURL:        d.BaseURL,
			APIKey:         d.APIKey,
			TimeoutSeconds: d.TimeoutSeconds,
		})
	}
	return out, cur.Err()
}

func (s *mongoStore) UpsertModel(ctx context.Context, model ModelSpec) error {
	if model.ID == "" || model.Provider == "" {
		return nil
	}
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"name":               model.Name,
			"provider":           model.Provider,
			"capabilities":       model.Capabilities,
			"upstream_model":     model.UpstreamModel,
			"max_output_tokens":  model.MaxOutputTokens,
			"updated_at":         now,
		},
		"$setOnInsert": bson.M{
			"created_at": now,
		},
	}
	_, err := s.models().UpdateOne(ctx, bson.M{"_id": model.ID}, update, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *mongoStore) DeleteModel(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	_, err := s.models().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *mongoStore) ListModels(ctx context.Context) ([]ModelSpec, error) {
	cur, err := s.models().Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []ModelSpec{}
	for cur.Next(ctx) {
		var d modelDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, ModelSpec{
			ID:              d.ID,
			Name:            d.Name,
			Provider:        d.Provider,
			Capabilities:    d.Capabilities,
			UpstreamModel:   d.UpstreamModel,
			MaxOutputTokens: d.MaxOutputTokens,
		})
	}
	return out, cur.Err()
}
