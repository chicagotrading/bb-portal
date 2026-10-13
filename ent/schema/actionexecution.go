package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ActionExecution is a spawn that was not a cache hit.
type ActionExecution struct {
	ent.Schema
}

// Fields of the ActionExecution.
func (ActionExecution) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("bazel_invocation_id").Immutable(),
		field.Int64("action_key_id").Immutable(),
		field.Int64("action_version_id").Immutable(),
		field.String("runner").Optional(),
		field.Int32("exit_code").Default(0),
		field.Int64("total_ms").Default(0),
		field.Int64("queue_ms").Default(0),
		field.Int64("exec_wall_ms").Default(0),
		field.Int64("fetch_ms").Default(0),
		field.Int64("upload_ms").Default(0),
		field.Int64("input_bytes").Default(0),
		field.Int64("input_files").Default(0),
		field.Int64("memory_peak_bytes").Default(0),
		// Start relative to the earliest spawn start of the log.
		field.Int64("start_offset_ms").Optional().Nillable(),
	}
}

// Edges of the ActionExecution.
func (ActionExecution) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("bazel_invocation", BazelInvocation.Type).
			Ref("action_executions").
			Field("bazel_invocation_id").
			Unique().
			Required().
			Immutable(),

		edge.From("action_key", ActionKey.Type).
			Ref("executions").
			Field("action_key_id").
			Unique().
			Required().
			Immutable(),

		edge.From("action_version", ActionVersion.Type).
			Ref("executions").
			Field("action_version_id").
			Unique().
			Required().
			Immutable(),
	}
}

// Indexes of the ActionExecution.
func (ActionExecution) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("bazel_invocation"),
		index.Edges("action_key", "bazel_invocation"),
		index.Edges("action_version"),
	}
}

// Annotations of the ActionExecution.
func (ActionExecution) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.Skip(entgql.SkipAll),
	}
}

// Mixin of the ActionExecution.
func (ActionExecution) Mixin() []ent.Mixin {
	return []ent.Mixin{
		Int64IdMixin{},
	}
}
