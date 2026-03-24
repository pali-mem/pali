package tenant

// IsTenantIsolated reports whether a resource belongs to the requesting tenant.
func IsTenantIsolated(requestTenantID, resourceTenantID string) bool {
	return requestTenantID != "" && requestTenantID == resourceTenantID
}
