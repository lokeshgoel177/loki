package events

// Canonical topic constants for the Loki Event Broker.
// Topics follow dot-separated hierarchical naming: <domain>.<entity>.<action>
const (
	// Session lifecycle topics
	TopicSessionCreated      = "session.created"
	TopicSessionStateChanged = "session.state_changed"
	TopicSessionFinished     = "session.finished"
	TopicSessionFailed       = "session.failed"
	TopicSessionCompacted    = "session.compacted"

	// LLM streaming & message topics
	TopicMessageDelta     = "message.delta"
	TopicMessageCompleted = "message.completed"

	// Tool execution topics
	TopicToolStarted   = "tool.started"
	TopicToolOutput    = "tool.output"
	TopicToolCompleted = "tool.completed"
	TopicToolError     = "tool.error"

	// Permission handshake topics
	TopicPermissionRequested = "permission.requested"
	TopicPermissionResolved  = "permission.resolved"

	// Client alerts & diagnostics
	TopicClientEventsDropped = "client.events_dropped"
)
