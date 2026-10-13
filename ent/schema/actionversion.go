package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ActionVersion is an action key with a specific action digest.
type ActionVersion struct {
	ent.Schema
}

// Fields of the ActionVersion.
func (ActionVersion) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("action_key_id").Immutable(),
		field.Bytes("digest_hash").Immutable(),
		field.Int64("digest_size_bytes").Immutable(),
		// Input fingerprint groups: group name to a short hash.
		field.JSON("groups", map[string]string{}).Optional(),
		field.Time("first_seen_at").Immutable(),
		// Bumped at most daily by ingestion; drives garbage collection.
		field.Time("last_seen_at"),
	}
}

// Edges of the ActionVersion.
func (ActionVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("action_key", ActionKey.Type).
			Ref("versions").
			Field("action_key_id").
			Unique().
			Required().
			Immutable(),

		edge.To("executions", ActionExecution.Type),
	}
}

// Indexes of the ActionVersion.
func (ActionVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("action_key_id", "digest_hash", "digest_size_bytes").
			Unique(),
		index.Fields("last_seen_at"),
	}
}

// Annotations of the ActionVersion.
func (ActionVersion) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.Skip(entgql.SkipAll),
	}
}

// Mixin of the ActionVersion.
func (ActionVersion) Mixin() []ent.Mixin {
	return []ent.Mixin{
		Int64IdMixin{},
	}
}
