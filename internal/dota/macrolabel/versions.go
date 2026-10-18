package macrolabel

const LabelerVersion = "macro-labeler-v0.2"

type SourceMode string

const (
	SourceModePlayerVisibleGSI SourceMode = "PLAYER_VISIBLE_GSI"
	SourceModeOracleReplay     SourceMode = "ORACLE_REPLAY"
)

type LabelStatus string

const (
	LabelStatusLabeled   LabelStatus = "LABELED"
	LabelStatusAmbiguous LabelStatus = "AMBIGUOUS"
	LabelStatusUnlabeled LabelStatus = "UNLABELED"
	LabelStatusExcluded  LabelStatus = "EXCLUDED"
)

type ExclusionReason string

const (
	ExclusionPreGame                  ExclusionReason = "PRE_GAME"
	ExclusionPostGame                 ExclusionReason = "POST_GAME"
	ExclusionSourceGap                ExclusionReason = "SOURCE_GAP"
	ExclusionStateUnavailable         ExclusionReason = "STATE_UNAVAILABLE"
	ExclusionRosterUnavailable        ExclusionReason = "ROSTER_UNAVAILABLE"
	ExclusionInvalidClock             ExclusionReason = "INVALID_CLOCK"
	ExclusionInsufficientFrameQuality ExclusionReason = "INSUFFICIENT_FRAME_QUALITY"
	ExclusionOther                    ExclusionReason = "OTHER"
)

type AmbiguityReason string

const (
	AmbiguityCloseScores         AmbiguityReason = "CLOSE_SCORES"
	AmbiguityMultipleTargets     AmbiguityReason = "MULTIPLE_TARGETS"
	AmbiguityTemporalTransition  AmbiguityReason = "TEMPORAL_TRANSITION"
	AmbiguityConflictingEvidence AmbiguityReason = "CONFLICTING_EVIDENCE"
	AmbiguityInsufficientMargin  AmbiguityReason = "INSUFFICIENT_MARGIN"
	AmbiguityMultipleSpecific    AmbiguityReason = "MULTIPLE_SPECIFIC_ACTIONS"
	AmbiguityOther               AmbiguityReason = "OTHER"
)
