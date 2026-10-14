package model

import "time"

// fork: O3 — organization plans reuse upstream's personal-plan period math.
func CalcSubscriptionPlanEndTime(start time.Time, durationUnit string, durationValue int, customSeconds int64) (int64, error) {
	return calcPlanEndTime(start, &SubscriptionPlan{DurationUnit: durationUnit, DurationValue: durationValue, CustomSeconds: customSeconds})
}

func CalcSubscriptionNextResetTime(base time.Time, resetPeriod string, resetCustomSeconds int64, endUnix int64) int64 {
	return calcNextResetTime(base, &SubscriptionPlan{QuotaResetPeriod: resetPeriod, QuotaResetCustomSeconds: resetCustomSeconds}, endUnix)
}
