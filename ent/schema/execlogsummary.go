package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ExecLogSummary is the ingestion result of an invocation's exec log.
type ExecLogSummary struct {
	ent.Schema
}

// Fields of the ExecLogSummary.
func (ExecLogSummary) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("bazel_invocation_id").Immutable(),
		field.Enum("status").Values("PENDING", "DONE", "FAILED"),
		field.String("error").Optional(),
		field.Int32("attempts").Default(0),
		field.Time("started_at"),
		field.Time("finished_at").Optional().Nillable(),
		// REv2 instance name and digest function of the action digests.
		field.String("rev2_instance_name").Optional(),
		field.Int16("digest_function").Optional(),
		field.Int64("spawn_count").Default(0),
		field.Int64("cache_hits").Default(0),
		field.Int64("executed_count").Default(0),
		field.Int64("parse_ms").Default(0),
		// Uncompressed size of the log.
		field.Int64("bytes").Default(0),
		// Every spawn's (key, version), see internal/actioninsights.
		field.Bytes("spawn_index").Optional(),
	}
}

// Edges of the ExecLogSummary.
func (ExecLogSummary) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("bazel_invocation", BazelInvocation.Type).
			Ref("exec_log_summary").
			Field("bazel_invocation_id").
			Unique().
			Required().
			Immutable(),
	}
}

// Indexes of the ExecLogSummary.
func (ExecLogSummary) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "started_at"),
	}
}

// Annotations of the ExecLogSummary.
func (ExecLogSummary) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.Skip(entgql.SkipAll),
	}
}

// Mixin of the ExecLogSummary.
func (ExecLogSummary) Mixin() []ent.Mixin {
	return []ent.Mixin{
		Int64IdMixin{},
	}
}
