package billing

// TaskBillingRevokeKey is the canonical asynq task type for delayed Unkey API
// key revocation. It is imported by both the Lemon Squeezy webhook handler
// (to enqueue the task) and the worker (to register the handler), ensuring
// the string value cannot drift between the two sides.
const TaskBillingRevokeKey = "billing:revoke-key"
