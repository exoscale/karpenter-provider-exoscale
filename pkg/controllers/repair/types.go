package repair

import (
	"time"
)

const (
	AnnotationRepairAttempts = "exoscale.com/repair-attempts"
	AnnotationLastRepairTime = "exoscale.com/last-repair-time"

	MaxRepairAttempts = 3
	RepairCooldown    = 10 * time.Minute
)
