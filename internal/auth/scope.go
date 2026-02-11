package auth

// AgentAllowed returns true if agentID is permitted by the allowed list.
// An empty or nil list means all agents are allowed.
func AgentAllowed(allowed []string, agentID string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == agentID {
			return true
		}
	}
	return false
}
