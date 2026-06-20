package floodguard

// VelocityKey builds the store key used for velocity rules from an account key
// and optional action label (e.g. "acct-1:withdraw").
func VelocityKey(key, action string) string {
	if action != "" {
		return key + ":" + action
	}
	return key
}

func velocityKey(req Request) string {
	return VelocityKey(req.Key, req.Action)
}
