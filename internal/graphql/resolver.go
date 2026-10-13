package graphql

import (
	"github.com/99designs/gqlgen/graphql"
	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/buildbarn/bb-portal/internal/database"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

// The Resolver Type for DI
type Resolver struct {
	db database.Client
	// spawnIndexes caches spawn indexes for action insights, if set.
	spawnIndexes *actioninsights.IndexCache
}

// NewSchema creates a graphql executable schema.
func NewSchema(db database.Client) graphql.ExecutableSchema {
	return NewExecutableSchema(Config{
		Resolvers: &Resolver{db: db, spawnIndexes: actioninsights.NewIndexCache(SpawnIndexCacheBytes)},
	})
}
