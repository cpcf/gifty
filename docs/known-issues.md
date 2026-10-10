# Pre-existing observation from the refactor

The scheduler's `tick` function (`internal/app/mail.go`) logs a save failure after queuing reminders and then continues selecting and sending due mail. If that save fails, delivery can occur before the queued state is persisted; a restart can therefore repeat notifications. This behaviour existed in the original `mail.go` and is unchanged by the structural refactor. Resolving it requires a separate decision about scheduler transaction and retry semantics.
