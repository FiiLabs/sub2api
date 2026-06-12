package service

const (
	DynamicRateModeOff               = "off"
	DynamicRateModeAccountPlusMargin = "account_plus_margin"
	DynamicRateModeAccountMarkup     = "account_markup"
)

// ResolveDynamicRateMultiplier returns the user-facing multiplier for a request.
// fixedMultiplier is the current group/user-specific multiplier. accountMultiplier
// is the selected upstream account cost multiplier.
func ResolveDynamicRateMultiplier(group *Group, fixedMultiplier, accountMultiplier float64) float64 {
	if group == nil || !group.DynamicRateEnabled {
		return fixedMultiplier
	}
	if accountMultiplier < 0 {
		accountMultiplier = 1.0
	}

	var effective float64
	switch group.DynamicRateMode {
	case DynamicRateModeAccountPlusMargin:
		effective = accountMultiplier + group.DynamicRateMargin
	case DynamicRateModeAccountMarkup:
		effective = accountMultiplier * (1 + group.DynamicRateMargin)
	default:
		return fixedMultiplier
	}

	if effective < 0 {
		effective = 0
	}
	if group.DynamicRateMinMultiplier != nil && effective < *group.DynamicRateMinMultiplier {
		effective = *group.DynamicRateMinMultiplier
	}
	if group.DynamicRateMaxMultiplier != nil && effective > *group.DynamicRateMaxMultiplier {
		effective = *group.DynamicRateMaxMultiplier
	}
	return effective
}

// ResolvePrecheckRateMultiplier returns the multiplier used before account
// selection. Dynamic groups use the configured ceiling so precheck is aligned
// with the highest possible user-facing request multiplier.
func ResolvePrecheckRateMultiplier(group *Group, fixedMultiplier float64) float64 {
	if group != nil && group.DynamicRateEnabled && group.DynamicRateMaxMultiplier != nil {
		if *group.DynamicRateMaxMultiplier < 0 {
			return 0
		}
		return *group.DynamicRateMaxMultiplier
	}
	if fixedMultiplier < 0 {
		return 0
	}
	return fixedMultiplier
}

func precheckMinimumCost(group *Group, fixedMultiplier float64) float64 {
	return 0.00000001 * ResolvePrecheckRateMultiplier(group, fixedMultiplier)
}

func groupRateMultiplier(group *Group) float64 {
	if group == nil {
		return 1
	}
	return group.RateMultiplier
}

func normalizeDynamicRateMode(mode string) string {
	switch mode {
	case DynamicRateModeAccountPlusMargin, DynamicRateModeAccountMarkup:
		return mode
	default:
		return DynamicRateModeOff
	}
}
