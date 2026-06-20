package main

import (
	"fmt"
	"time"
)

// rateLimitBackoffThreshold is how few GraphQL points must remain before the
// background auto-refresh poll pauses itself until the rate-limit window resets.
// It is a deliberate safety buffer below GitHub's 5000-points/hour GraphQL
// ceiling: the remaining count is global to the token, so every gh-zen instance
// sharing that token reads the same depleting number and backs off in concert
// before any of them hits the wall.
const rateLimitBackoffThreshold = 100

// rateLimitedNow reports whether the background auto-refresh poll should hold off
// right now: a real reading has landed, the remaining GraphQL budget is under
// the threshold, and the window has not yet reset. Once the reset time passes it
// returns false even without a fresh reading, so the next tick polls again and
// refreshes the gauge. Only the background poll consults this; user-initiated
// fetches (opening an item, paging) still go through so the UI stays responsive.
func (m model) rateLimitedNow() (rateLimitSnapshot, bool) {
	if m.conn == nil {
		return rateLimitSnapshot{}, false
	}
	rl := m.conn.rateLimitState()
	return rl, rl.valid && rl.remaining < rateLimitBackoffThreshold && time.Now().Before(rl.resetAt)
}

// rateLimitNotice is the status-bar message shown while the poll is backed off,
// or "" when it is not. It names the local clock time the poll resumes (the
// window reset) so the pause reads as deliberate rather than a hang.
func (m model) rateLimitNotice() string {
	rl, ok := m.rateLimitedNow()
	if !ok {
		return ""
	}
	return fmt.Sprintf("rate limit low · auto-refresh resumes %s", rl.resetAt.Local().Format("15:04"))
}
