package authz

const (
	ResourceInvitation   = "invitation"
	ResourceModelPricing = "model_pricing"
)

var (
	InvitationRead    = Permission{Resource: ResourceInvitation, Action: ActionRead}
	ModelPricingWrite = Permission{Resource: ResourceModelPricing, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceInvitation, LabelKey: "Invitation Records", Actions: []ActionDefinition{{Action: ActionRead, LabelKey: "Read invitation records", DescriptionKey: "View invitation and rebate records for all users.", DefaultRoles: []string{BuiltInRoleAdmin}}}})
	RegisterResource(ResourceDefinition{Resource: ResourceModelPricing, LabelKey: "Model pricing", Actions: []ActionDefinition{{Action: ActionWrite, LabelKey: "Save model pricing", DescriptionKey: "Save model metadata and its pricing together.", DefaultRoles: []string{BuiltInRoleAdmin}}}})
}
