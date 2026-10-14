package authz

// fork: O3 — platform-level organization administration (root by default; no
// DefaultRoles means the admin built-in role does not receive it).
const ResourceOrganization = "organization"

var (
	OrganizationRead  = Permission{Resource: ResourceOrganization, Action: ActionRead}
	OrganizationWrite = Permission{Resource: ResourceOrganization, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceOrganization,
		LabelKey: "Organization Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read organizations", DescriptionKey: "List organizations, their owners and wallet balances."},
			{Action: ActionWrite, LabelKey: "Manage organizations", DescriptionKey: "Create, edit, fund and delete organizations, and expire their abandoned top-up orders."},
		},
	})
}
