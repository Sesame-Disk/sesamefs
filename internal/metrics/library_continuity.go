package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	LibraryContinuityCertificationRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "library_continuity_certifications_total",
			Help: "Library baseline certification attempts by outcome and reason.",
		},
		[]string{"outcome", "reason"},
	)

	LibraryContinuityCertificationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "library_continuity_certification_duration_seconds",
			Help:    "Time spent certifying one library baseline.",
			Buckets: prometheus.DefBuckets,
		},
	)

	LibraryContinuityCertificationCommitsWalkedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "library_continuity_certification_commits_walked_total",
			Help: "Commits inspected by library baseline certification.",
		},
	)

	LibraryContinuityCertificationFSObjectsWalkedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "library_continuity_certification_fs_objects_walked_total",
			Help: "FS objects inspected by library baseline certification.",
		},
	)

	LibraryContinuityCertificationUniqueBlocksTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "library_continuity_certification_unique_blocks_total",
			Help: "Unique blocks inspected by library baseline certification.",
		},
	)

	LibraryContinuityCertificationPermanentLivenessWritesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "library_continuity_certification_permanent_liveness_writes_total",
			Help: "Permanent block-reference writes performed by certification.",
		},
	)

	LibraryContinuityCertificationPhysicalRevalidationsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "library_continuity_certification_physical_revalidations_total",
			Help: "Exact physical-authority revalidations performed by certification.",
		},
	)

	BlockMappingAuthorityPromotionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "block_mapping_authority_promotions_total",
			Help: "Cold-path SHA-1 to SHA-256 mapping-authority promotions by outcome.",
		},
		[]string{"outcome"},
	)

	LibraryContinuityMappingAuthorityDivergenceTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "library_continuity_mapping_authority_divergence_total",
			Help: "SHA-1 dependencies whose mutable block_id_mappings row disagreed with the durable mapping authority during certification.",
		},
	)
)
