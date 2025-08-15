package metrics

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// Collector contains the state of all of the L4 LBs in the cluster
type Collector struct {
	// l4ILBServiceLegacyMap is a map between service key and L4 ILB service state. It's used in the legacy metric.
	l4ILBServiceLegacyMap map[string]L4ILBServiceLegacyState
	// l4NetLBServiceLegacyMap is a map between service key and L4 NetLB service state. It's used in the legacy metric.
	l4NetLBServiceLegacyMap map[string]L4NetLBServiceLegacyState
	// l4NetLBProvisionDeadlineForLegacyMetric is a time after which a failing NetLB will be marked as persistent error in the legacy metric.
	l4NetLBProvisionDeadlineForLegacyMetric time.Duration
	// l4ILBServiceMap is a map between service key and L4 ILB service state.
	l4ILBServiceMap map[string]L4ServiceState
	// l4NetLBServiceMap is a map between service key and L4 NetLB service state.
	l4NetLBServiceMap map[string]L4ServiceState

	sync.Mutex

	// duration between metrics exports
	exportsInterval time.Duration

	enableILBDualStack   bool
	enableNetLBDualStack bool

	logger klog.Logger
}

// NewCollector initializes Collector for gathering L4 metrics
func NewCollector(exportInterval, l4NetLBProvisionDeadline time.Duration, enableNetLBDualStack, enableILBDualStack bool, logger klog.Logger) *Collector {
	return &Collector{
		exportsInterval:                         exportInterval,
		l4ILBServiceLegacyMap:                   make(map[string]L4ILBServiceLegacyState),
		l4NetLBServiceLegacyMap:                 make(map[string]L4NetLBServiceLegacyState),
		l4ILBServiceMap:                         make(map[string]L4ServiceState),
		l4NetLBServiceMap:                       make(map[string]L4ServiceState),
		l4NetLBProvisionDeadlineForLegacyMetric: l4NetLBProvisionDeadline,
		enableILBDualStack:                      enableILBDualStack,
		enableNetLBDualStack:                    enableNetLBDualStack,
		logger:                                  logger.WithName("L4MetricsCollector"),
	}
}

// NewFakeCollector creates an L4LB Collector for unit tests with default values
func NewFakeCollector() *Collector {
	return NewCollector(10*time.Minute, 20*time.Minute, true, true, klog.TODO())
}

// Run starts exporting metrics periodically
func (c *Collector) Run(stopCh <-chan struct{}) {
	c.logger.V(3).Info("L4 Contoller Metrics Collector initialized",
		"exportInterval", c.exportsInterval,
		"l4NetLBProvisionDeadlineForLegacyMetric", c.l4NetLBProvisionDeadlineForLegacyMetric,
		"enableILBDualStack", c.enableILBDualStack,
		"enableNetLBDualStack", c.enableNetLBDualStack,
	)

	go func() {
		time.Sleep(c.exportsInterval)
		wait.Until(c.export, c.exportsInterval, stopCh)
	}()
	<-stopCh
}

func (c *Collector) export() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error(nil, "failed to export metrics due to a panic", "recoverMessage", r)
		}
	}()

	c.exportIPv4()
	c.exportDualStack()
	c.exportLegacy()
}
