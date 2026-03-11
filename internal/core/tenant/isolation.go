package tenant

func IsTenantIsolated(requestTenantID, resourceTenantID string) bool {
	return requestTenantID != "" && requestTenantID == resourceTenantID
}
