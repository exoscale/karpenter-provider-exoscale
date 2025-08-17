package metrics

import (
	opmetrics "github.com/awslabs/operatorpkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/karpenter/pkg/metrics"
)

const (
	CloudProviderSubsystem = "cloudprovider"
	InstanceTypeLabel      = "instance_type"
	ZoneLabel              = "zone"
	NodeClassLabel         = "nodeclass"
	OperationLabel         = "operation"
	ErrorTypeLabel         = "error_type"
	CacheTypeLabel         = "cache_type"
)

var (
	InstanceTypesDiscovered = opmetrics.NewPrometheusGauge(
		crmetrics.Registry,
		prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "instance_types_discovered",
			Help:      "Number of instance types discovered from Exoscale API",
		},
		[]string{ZoneLabel},
	)

	InstanceTypeMemory = opmetrics.NewPrometheusGauge(
		crmetrics.Registry,
		prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "instance_type_memory_bytes",
			Help:      "Memory, in bytes, for a given instance type",
		},
		[]string{InstanceTypeLabel},
	)

	InstanceTypeCPU = opmetrics.NewPrometheusGauge(
		crmetrics.Registry,
		prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "instance_type_cpu_cores",
			Help:      "CPU cores for a given instance type",
		},
		[]string{InstanceTypeLabel},
	)

	InstanceTypePriceEstimate = opmetrics.NewPrometheusGauge(
		crmetrics.Registry,
		prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "instance_type_price_estimate",
			Help:      "Estimated hourly price (EUR) for a given instance type",
		},
		[]string{
			InstanceTypeLabel,
			ZoneLabel,
		},
	)

	APICallsTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "api_calls_total",
			Help:      "Total number of API calls to Exoscale",
		},
		[]string{
			OperationLabel,
		},
	)

	APICallErrorsTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "api_call_errors_total",
			Help:      "Total number of API call errors to Exoscale",
		},
		[]string{
			OperationLabel,
			ErrorTypeLabel,
		},
	)

	APICallDurationSeconds = opmetrics.NewPrometheusSummary(
		crmetrics.Registry,
		prometheus.SummaryOpts{
			Namespace:  metrics.Namespace,
			Subsystem:  CloudProviderSubsystem,
			Name:       "api_call_duration_seconds",
			Help:       "Duration of API calls to Exoscale",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{
			OperationLabel,
		},
	)

	InstancesLaunchedTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "instances_launched_total",
			Help:      "Total number of instances launched by Exoscale provider",
		},
		[]string{
			InstanceTypeLabel,
			ZoneLabel,
			NodeClassLabel,
		},
	)

	InstanceLaunchDurationSeconds = opmetrics.NewPrometheusSummary(
		crmetrics.Registry,
		prometheus.SummaryOpts{
			Namespace:  metrics.Namespace,
			Subsystem:  CloudProviderSubsystem,
			Name:       "instance_launch_duration_seconds",
			Help:       "Duration of instance launch operations",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{
			InstanceTypeLabel,
			ZoneLabel,
		},
	)

	BootstrapTokensCreatedTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "bootstrap_tokens_created_total",
			Help:      "Total number of bootstrap tokens created",
		},
		[]string{},
	)

	NetworkAttachmentsTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "network_attachments_total",
			Help:      "Total number of private network attachment operations",
		},
		[]string{},
	)

	NetworkAttachmentErrorsTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "network_attachment_errors_total",
			Help:      "Total number of private network attachment errors",
		},
		[]string{},
	)

	OrphanedInstancesCount = opmetrics.NewPrometheusGauge(
		crmetrics.Registry,
		prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "orphaned_instances_count",
			Help:      "Current count of orphaned instances awaiting garbage collection",
		},
		[]string{},
	)

	OrphanedInstancesCleanedTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "orphaned_instances_cleaned_total",
			Help:      "Total number of orphaned instances cleaned by garbage collector",
		},
		[]string{},
	)

	CacheHitsTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		},
		[]string{
			CacheTypeLabel,
		},
	)

	CacheMissesTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		},
		[]string{
			CacheTypeLabel,
		},
	)

	DriftDetectedTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "drift_detected_total",
			Help:      "Total number of instances where drift was detected",
		},
		[]string{
			metrics.ReasonLabel,
		},
	)

	RepairActionsTotal = opmetrics.NewPrometheusCounter(
		crmetrics.Registry,
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: CloudProviderSubsystem,
			Name:      "repair_actions_total",
			Help:      "Total number of repair actions taken on instances",
		},
		[]string{
			metrics.ReasonLabel,
		},
	)
)
