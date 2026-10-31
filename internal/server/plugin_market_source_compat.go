package server

// Source compatibility is an explicit alternative supplied by the catalog.
// It never changes the host's release identity or an older release's minimum.
type marketSourceCompatibility struct {
	MinAideVersion string   `json:"minAideVersion"`
	Features       []string `json:"features"`
}

func marketCompatibleForRuntime(p marketPlugin, version, mode string) (bool, string) {
	ok, reason := marketCompatible(p, version)
	if ok || mode != "source" || p.SourceCompatibility == nil {
		return ok, reason
	}
	source := p.SourceCompatibility
	if len(source.Features) == 0 {
		return false, reason
	}
	for _, feature := range source.Features {
		switch feature {
		case "provider-account-quota-v1":
			// This build implements the generic plugin quota catalog contract.
		case "provider-support-usd-v1":
			if !validSupportCurrency("USD") {
				return false, "当前 Aide 源码构建缺少插件要求的能力"
			}
		default:
			return false, "当前 Aide 源码构建缺少插件要求的能力"
		}
	}
	// Preserve all normal feature checks and apply the explicit source floor.
	p.MinAideVersion = source.MinAideVersion
	return marketCompatible(p, version)
}
