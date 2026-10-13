package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ActionKey identifies an action across invocations.
type ActionKey struct {
	ent.Schema
}

// Fields of the ActionKey.
func (ActionKey) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("instance_name_id").Immutable(),
		field.String("target_label").Immutable(),
		field.String("mnemonic").Immutable(),
		// Path of the first output, including the configuration segment.
		field.String("primary_output").Immutable(),
	}
}

// Edges of the ActionKey.
func (ActionKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("instance_name", InstanceName.Type).
			Ref("action_keys").
			Field("instance_name_id").
			Unique().
			Required().
			Immutable(),

		edge.To("versions", ActionVersion.Type),

		edge.To("executions", ActionExecution.Type),
	}
}

// Indexes of the ActionKey.
func (ActionKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("instance_name_id", "target_label", "mnemonic", "primary_output").
			Unique(),
	}
}

// Annotations of the ActionKey.
func (ActionKey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.Skip(entgql.SkipAll),
	}
}

// Mixin of the ActionKey.
func (ActionKey) Mixin() []ent.Mixin {
	return []ent.Mixin{
		Int64IdMixin{},
	}
}
