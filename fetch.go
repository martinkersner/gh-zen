package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	gh "github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

// --- GitHub integration ---

// graphQLClient is the minimal slice of *api.GraphQLClient the fetch layer
// actually uses: a single Do(query, variables, &response) call. Depending on
// this interface (rather than the concrete client) lets tests inject a fake
// that returns canned responses/errors so the query-building and
// response-parsing logic can be exercised fully offline.
type graphQLClient interface {
	Do(query string, variables map[string]interface{}, response interface{}) error
}

// repoInfo is the resolved owner/name of the repository the fetch layer queries
// against. It mirrors the fields used from repository.Repository so tests can
// supply a fixed repo without reading `gh` config / resolving a git remote.
type repoInfo struct {
	Owner string
	Name  string
}

// newGraphQLClient and currentRepo are the injection seams for the GitHub I/O
// layer. Production wires the real client/repo resolution (api default client,
// repository.Current); tests swap them for fakes returning canned data/errors.
// Kept as package vars to match the existing fetchIssuesAndPRs/ghDiff pattern.
var (
	newGraphQLClient = func() (graphQLClient, error) {
		return api.DefaultGraphQLClient()
	}
	currentRepo = func() (repoInfo, error) {
		repo, err := repository.Current()
		if err != nil {
			return repoInfo{}, err
		}
		return repoInfo{Owner: repo.Owner, Name: repo.Name}, nil
	}
)

// githubConn memoizes the per-session GraphQL client and resolved repo so the
// fetch layer resolves them once instead of on every body/label/list round-trip
// (repository.Current shells out to git; client construction reads gh config).
// One conn is created per model (newModel) and shared by value-copies of the
// model via its pointer, so a single resolution is reused across every fetch and
// auto-refresh tick. Resolution is cached only on success and retried on failure
// (a transient auth/repo error doesn't poison the session), and resolve() is
// safe to call from the concurrent fetch goroutines.
type githubConn struct {
	mu     sync.Mutex
	client graphQLClient
	repo   repoInfo
	// rl is the most recent GraphQL rate-limit reading pulled from a successful
	// list-level query (see setRateLimit). It is read on the UI goroutine to gate
	// the auto-refresh poll and render the status-bar notice; the mutex guards it
	// against the concurrent fetch goroutines that write it.
	rl rateLimitSnapshot
}

// rateLimitNode decodes the `rateLimit { remaining resetAt }` selection appended
// (as a sibling of `repository`) to the list-level polling queries. The
// `rateLimit` field is itself free — it costs 0 GraphQL points — so reading it
// every round-trip adds no API cost. resetAt is GitHub's RFC3339 window-reset
// timestamp, which encoding/json parses straight into a time.Time.
type rateLimitNode struct {
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
}

// rateLimitSnapshot is the latest rate-limit reading the fetch layer has seen.
// valid stays false until a real reading lands, so the backoff gate is inert
// until then — test fakes that omit the rateLimit field (zero resetAt) never
// flip it on (see setRateLimit).
type rateLimitSnapshot struct {
	remaining int
	resetAt   time.Time
	valid     bool
}

// setRateLimit records a reading from a successful query. A zero resetAt means
// the field was absent (or a fake omitted it); such readings are dropped so the
// backoff gate never trips on synthetic/empty data.
func (c *githubConn) setRateLimit(n rateLimitNode) {
	if n.ResetAt.IsZero() {
		return
	}
	c.mu.Lock()
	c.rl = rateLimitSnapshot{remaining: n.Remaining, resetAt: n.ResetAt, valid: true}
	c.mu.Unlock()
}

// rateLimitState returns the latest snapshot. Safe to call from the UI goroutine
// while fetch goroutines write via setRateLimit.
func (c *githubConn) rateLimitState() rateLimitSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rl
}

// resolve returns the memoized client+repo, resolving them via the
// newGraphQLClient/currentRepo seams on first success. A failure is returned
// without being cached so a later call retries.
func (c *githubConn) resolve() (graphQLClient, repoInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return c.client, c.repo, nil
	}
	client, err := newGraphQLClient()
	if err != nil {
		return nil, repoInfo{}, err
	}
	repo, err := currentRepo()
	if err != nil {
		return nil, repoInfo{}, err
	}
	c.client, c.repo = client, repo
	return c.client, c.repo, nil
}

type dataMsg struct {
	issues []list.Item
	prs    []list.Item
	// Total open issues/PRs on GitHub (connection totalCount), which can exceed
	// the fetched node count since the query caps at listPageSize. The tabs
	// display these so the bracket reflects the repo's real open count, not just
	// what was fetched. Zero on the test-constructed dataMsgs that omit them;
	// tabCount falls back to the fetched length in that case.
	issueTotal int
	prTotal    int
	// Pagination state for the first page of each tab: the cursor to pass as the
	// next `after`, and whether more pages exist. Consumed by the dataMsg handler
	// to arm lazy "load more" on scroll. Zero-valued on test dataMsgs (hasNext
	// false → pagination simply never triggers).
	issueEndCursor   string
	issueHasNextPage bool
	prEndCursor      string
	prHasNextPage    bool
}

// moreDataMsg delivers one appended page for a single tab, fetched lazily when
// the cursor nears the end of the loaded items. Unlike dataMsg (which replaces
// the list), its items are appended to what's already loaded.
type moreDataMsg struct {
	tab         tab
	items       []list.Item
	endCursor   string
	hasNextPage bool
	err         error
}

// refreshedItem is the freshly-fetched mutable state of one already-loaded row.
type refreshedItem struct {
	title  string
	closed bool
}

// visibleRefreshMsg delivers an in-place refresh of the on-screen rows for a
// single tab, keyed by issue/PR number. It backs the deep auto-refresh: once
// extra pages are loaded the background tick refreshes just the visible window
// (by number) and patches title/closed in place, rather than refetching the
// whole list (which would reorder it and reset pagination). items is nil/empty
// when nothing came back.
type visibleRefreshMsg struct {
	tab   tab
	items map[int]refreshedItem
	err   error
}

type bodyMsg struct {
	key    string
	body   string
	author string
	labels []label
	err    error
	// prefetch marks a cheap list-body prefetch (no comments) as opposed to a
	// full fetch (body + comments). A prefetch must not overwrite a key that a
	// full fetch already populated, or a tick-driven prefetch landing after the
	// full fetch would drop the comments. See the bodyMsg handler.
	prefetch bool
}

type diffMsg struct {
	key  string
	diff string
	err  error
}

// labelTarget identifies one issue/PR whose labels the visible-window prefetch
// should pull. number is the issue/PR number; isPR selects the aliased
// pullRequest vs issue field in the batched query (see fetchLabels). key is the
// bodyCache key (cacheKey) the merged labels are stored under.
type labelTarget struct {
	key    string
	number int
	isPR   bool
}

// labelsMsg carries the result of a batched visible-window label prefetch:
// bodyCache keys mapped to the labels pulled for them. It is merged into the
// body cache (and the list items) without clobbering a full fetch's labels (see
// the labelsMsg handler). A failed batch is silently dropped (err) since the
// per-item detail fetch still pulls labels on open.
type labelsMsg struct {
	labels map[string][]label
	err    error
}

type errMsg struct {
	err error
}

// closeIssueResultMsg carries the result of a close-issue mutation (see
// cmdCloseIssue). number identifies the issue that was closed so the handler can
// reflect the new state on the matching list item / open detail; err is non-nil
// when the `gh issue close` call failed (surfaced via the model's err field
// without crashing the TUI).
type closeIssueResultMsg struct {
	number int
	err    error
}

// commentsFetchLimit caps how many issue/PR conversation comments are pulled
// per detail view. Bounded so a long thread can't make the single round-trip
// (and the resulting markdown render) unbounded.
const commentsFetchLimit = 50

// comment is a single conversation comment (author login + markdown body) on an
// issue or PR, in the order GitHub returns it (chronological).
type comment struct {
	author string
	body   string
}

// labelsFetchLimit caps how many labels are pulled per detail view. Issues/PRs
// rarely carry more than a handful; the cap bounds the chip row and the
// round-trip.
const labelsFetchLimit = 10

// fetchBody pulls the current body, conversation comments, and labels for a
// single issue or PR from GitHub. It is a blocking call meant to run inside a
// tea.Cmd. Kept separate from the list fetch so the detail diff (fetchDiff) can
// grow independently. For PRs this is the issue-comment thread; review comments
// live under a separate connection and are out of scope here.
//
// The returned int is the thread's total comment count (the connection's
// totalCount), which can exceed the number of comments returned when the thread
// is longer than commentsFetchLimit; callers use it to surface truncation. The
// labels carry their GitHub hex color for rendering as chips in the detail view.
// The returned author is the issue/PR opener's login ("" if GitHub returns a
// null author, e.g. a deleted account), rendered in the detail header.
func fetchBody(client graphQLClient, repo repoInfo, number int, isPR bool) (string, []comment, int, []label, string, error) {
	field := "issue"
	if isPR {
		field = "pullRequest"
	}
	query := fmt.Sprintf(`
		query($owner: String!, $repo: String!, $number: Int!, $comments: Int!, $labels: Int!) {
			repository(owner: $owner, name: $repo) {
				%s(number: $number) {
					body
					author { login }
					labels(first: $labels) {
						nodes {
							name
							color
						}
					}
					comments(first: $comments) {
						totalCount
						nodes {
							author { login }
							body
						}
					}
				}
			}
		}
	`, field)
	variables := map[string]interface{}{
		"owner":    repo.Owner,
		"repo":     repo.Name,
		"number":   number,
		"comments": commentsFetchLimit,
		"labels":   labelsFetchLimit,
	}

	type commentNode struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body string `json:"body"`
	}
	type labelNode struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	type detail struct {
		Body   string `json:"body"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels struct {
			Nodes []labelNode `json:"nodes"`
		} `json:"labels"`
		Comments struct {
			TotalCount int           `json:"totalCount"`
			Nodes      []commentNode `json:"nodes"`
		} `json:"comments"`
	}
	type response struct {
		Repository struct {
			Issue       detail `json:"issue"`
			PullRequest detail `json:"pullRequest"`
		} `json:"repository"`
	}
	var resp response
	if err := client.Do(query, variables, &resp); err != nil {
		return "", nil, 0, nil, "", err
	}
	d := resp.Repository.Issue
	if isPR {
		d = resp.Repository.PullRequest
	}
	comments := make([]comment, 0, len(d.Comments.Nodes))
	for _, n := range d.Comments.Nodes {
		comments = append(comments, comment{author: n.Author.Login, body: n.Body})
	}
	var labels []label
	for _, n := range d.Labels.Nodes {
		labels = append(labels, label{name: n.Name, color: n.Color})
	}
	return d.Body, comments, d.Comments.TotalCount, labels, d.Author.Login, nil
}

// fetchLabels pulls just the labels for a batch of issues/PRs in a single
// GraphQL round-trip, using aliased issue(number:)/pullRequest(number:) fields
// (n0, n1, ...). It backs the scroll-aware visible-window prefetch (model.go) so
// the first open of an on-screen item renders its label chips from cache rather
// than waiting on the per-item detail fetch. It is a blocking call meant to run
// inside a tea.Cmd.
//
// The returned map is keyed by each target's bodyCache key (cacheKey form) so
// the caller can merge it straight into bodyCache. Targets with no labels are
// simply absent from the map. An empty targets slice is a no-op (nil map, nil
// error) so the caller needn't special-case an empty visible window.
func fetchLabels(client graphQLClient, repo repoInfo, targets []labelTarget) (map[string][]label, error) {
	if len(targets) == 0 {
		return nil, nil
	}

	// Build one aliased selection per target: nN: issue|pullRequest(number: N).
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!) {\n")
	b.WriteString("\trepository(owner: $owner, name: $repo) {\n")
	for i, t := range targets {
		field := "issue"
		if t.isPR {
			field = "pullRequest"
		}
		fmt.Fprintf(&b, "\t\tn%d: %s(number: %d) { labels(first: %d) { nodes { name color } } }\n", i, field, t.number, labelsFetchLimit)
	}
	b.WriteString("\t}\n}")
	query := b.String()
	variables := map[string]interface{}{
		"owner": repo.Owner,
		"repo":  repo.Name,
	}

	// The aliased fields share the same shape, so unmarshal the repository object
	// into a generic alias->detail map and pull each target's labels back out by
	// its alias.
	type labelNode struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	type aliased struct {
		Labels struct {
			Nodes []labelNode `json:"nodes"`
		} `json:"labels"`
	}
	type response struct {
		Repository map[string]aliased `json:"repository"`
	}
	var resp response
	if err := client.Do(query, variables, &resp); err != nil {
		return nil, err
	}

	out := make(map[string][]label)
	for i, t := range targets {
		a, ok := resp.Repository[fmt.Sprintf("n%d", i)]
		if !ok {
			continue
		}
		var labels []label
		for _, n := range a.Labels.Nodes {
			labels = append(labels, label{name: n.Name, color: n.Color})
		}
		if labels != nil {
			out[t.key] = labels
		}
	}
	return out, nil
}

// ghDiff shells out to `gh pr diff <number>` and returns its stdout. It is a
// package var so tests can stub the network/`gh` call and keep the diff plumbing
// hermetic.
var ghDiff = func(number int) (string, error) {
	stdout, stderr, err := gh.Exec("pr", "diff", strconv.Itoa(number))
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s: %w", msg, err)
		}
		return "", err
	}
	return stdout.String(), nil
}

// fetchDiff pulls a single PR's diff from GitHub via `gh pr diff`. It is a
// blocking call meant to run inside a tea.Cmd.
func fetchDiff(number int) (string, error) {
	return ghDiff(number)
}

// ghOpenInBrowser shells out to `gh <issue|pr> view <number> --web`, which opens
// the item's GitHub page in the default browser and returns promptly. itemType
// is "issue" or "pr". It is a package var so tests can stub it and assert the
// open without launching a real browser (mirroring the ghDiff seam).
var ghOpenInBrowser = func(itemType string, number int) error {
	_, stderr, err := gh.Exec(itemType, "view", strconv.Itoa(number), "--web")
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}

// ghCloseIssue shells out to `gh issue close <number> --reason <reason>`, closing
// the issue with the given GitHub state reason ("completed" or "not planned").
// It is a package var so tests can stub the `gh` call and assert the close
// without hitting the network (mirroring the ghDiff / ghOpenInBrowser seams).
var ghCloseIssue = func(number int, reason string) error {
	_, stderr, err := gh.Exec("issue", "close", strconv.Itoa(number), "--reason", reason)
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}

// colorizeDiff applies green/red foreground colors to added/removed diff lines
// (leaving hunk headers and context lines unstyled) so the diff is readable in
// the detail viewport. Plain-text rendering is the fallback when the input has
// no recognizable diff markers.
func colorizeDiff(diff string) string {
	if diff == "" {
		return ""
	}
	addStyle := lipgloss.NewStyle().Foreground(diffAddColor)
	delStyle := lipgloss.NewStyle().Foreground(diffDelColor)
	metaStyle := lipgloss.NewStyle().Foreground(accentColor)
	lines := strings.Split(diff, "\n")
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
			lines[i] = metaStyle.Render(ln)
		case strings.HasPrefix(ln, "@@"):
			lines[i] = metaStyle.Render(ln)
		case strings.HasPrefix(ln, "+"):
			lines[i] = addStyle.Render(ln)
		case strings.HasPrefix(ln, "-"):
			lines[i] = delStyle.Render(ln)
		}
	}
	return strings.Join(lines, "\n")
}

// listPageSize is how many issues/PRs are fetched per page, both on the initial
// load and on each lazy "load more" as the cursor nears the end of the list.
// GitHub caps connection `first` at 100.
const listPageSize = 50

// listNode is one issue/PR row as returned by the list queries.
type listNode struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// pageInfo carries cursor pagination state from a connection.
type pageInfo struct {
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

func listItemsFromNodes(nodes []listNode, type_ string) []list.Item {
	var items []list.Item
	for _, n := range nodes {
		items = append(items, item{number: n.Number, title: n.Title, body: n.Body, type_: type_, author: n.Author.Login})
	}
	return items
}

// fetchIssuesAndPRs returns the cmd that loads the first page of the issue/PR
// lists from GitHub. It is a package var (like ghDiff) so tests can swap in a
// hermetic data source and drive the program offline without hitting the
// network. The conn supplies the memoized client+repo (resolved inside the
// returned cmd's goroutine, off the UI thread, so a refresh tick reuses the
// first resolution). pageInfo is requested so the dataMsg handler can arm lazy
// pagination; see fetchMoreItems for the per-tab next-page fetch.
var fetchIssuesAndPRs = func(conn *githubConn) tea.Cmd {
	return func() tea.Msg {
		client, repo, err := conn.resolve()
		if err != nil {
			return errMsg{err}
		}

		query := `
			query($owner: String!, $repo: String!, $first: Int!) {
				rateLimit { remaining resetAt }
				repository(owner: $owner, name: $repo) {
					issues(first: $first, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						totalCount
						pageInfo { endCursor hasNextPage }
						nodes {
							number
							title
							body
							author { login }
						}
					}
					pullRequests(first: $first, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						totalCount
						pageInfo { endCursor hasNextPage }
						nodes {
							number
							title
							body
							author { login }
						}
					}
				}
			}
		`
		variables := map[string]interface{}{
			"owner": repo.Owner,
			"repo":  repo.Name,
			"first": listPageSize,
		}

		type response struct {
			RateLimit  rateLimitNode `json:"rateLimit"`
			Repository struct {
				Issues struct {
					TotalCount int        `json:"totalCount"`
					PageInfo   pageInfo   `json:"pageInfo"`
					Nodes      []listNode `json:"nodes"`
				} `json:"issues"`
				PullRequests struct {
					TotalCount int        `json:"totalCount"`
					PageInfo   pageInfo   `json:"pageInfo"`
					Nodes      []listNode `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}

		var resp response
		if err := client.Do(query, variables, &resp); err != nil {
			return errMsg{err}
		}
		conn.setRateLimit(resp.RateLimit)

		return dataMsg{
			issues:           listItemsFromNodes(resp.Repository.Issues.Nodes, "issue"),
			prs:              listItemsFromNodes(resp.Repository.PullRequests.Nodes, "pr"),
			issueTotal:       resp.Repository.Issues.TotalCount,
			prTotal:          resp.Repository.PullRequests.TotalCount,
			issueEndCursor:   resp.Repository.Issues.PageInfo.EndCursor,
			issueHasNextPage: resp.Repository.Issues.PageInfo.HasNextPage,
			prEndCursor:      resp.Repository.PullRequests.PageInfo.EndCursor,
			prHasNextPage:    resp.Repository.PullRequests.PageInfo.HasNextPage,
		}
	}
}

// fetchMoreItems returns the cmd that loads the next page for a single tab,
// starting after the given cursor. It queries only the one connection (the other
// tab's cursor isn't advancing) and returns a moreDataMsg whose items are
// appended to the already-loaded list. It is a package var so tests can stub it.
var fetchMoreItems = func(conn *githubConn, t tab, after string) tea.Cmd {
	return func() tea.Msg {
		client, repo, err := conn.resolve()
		if err != nil {
			return moreDataMsg{tab: t, err: err}
		}

		field := "issues"
		type_ := "issue"
		if t == tabPRs {
			field = "pullRequests"
			type_ = "pr"
		}
		query := `
			query($owner: String!, $repo: String!, $first: Int!, $after: String!) {
				rateLimit { remaining resetAt }
				repository(owner: $owner, name: $repo) {
					` + field + `(first: $first, after: $after, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						pageInfo { endCursor hasNextPage }
						nodes {
							number
							title
							body
							author { login }
						}
					}
				}
			}
		`
		variables := map[string]interface{}{
			"owner": repo.Owner,
			"repo":  repo.Name,
			"first": listPageSize,
			"after": after,
		}

		type conn_ struct {
			PageInfo pageInfo   `json:"pageInfo"`
			Nodes    []listNode `json:"nodes"`
		}
		// The connection field name varies, so decode the repository object into a
		// map of the two possible keys and pick the populated one.
		type response struct {
			RateLimit  rateLimitNode `json:"rateLimit"`
			Repository struct {
				Issues       conn_ `json:"issues"`
				PullRequests conn_ `json:"pullRequests"`
			} `json:"repository"`
		}

		var resp response
		if err := client.Do(query, variables, &resp); err != nil {
			return moreDataMsg{tab: t, err: err}
		}
		conn.setRateLimit(resp.RateLimit)

		c := resp.Repository.Issues
		if t == tabPRs {
			c = resp.Repository.PullRequests
		}
		return moreDataMsg{
			tab:         t,
			items:       listItemsFromNodes(c.Nodes, type_),
			endCursor:   c.PageInfo.EndCursor,
			hasNextPage: c.PageInfo.HasNextPage,
		}
	}
}

// mineSearchNode is one node from a `search(type: ISSUE)` connection used by the
// "mine only" scope. Each mine search is scoped to a single item type (is:issue
// or is:pr), so every node is that type and carries no __typename discriminator.
type mineSearchNode struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// mineSearchQuery builds the GitHub search query string for one tab of the "mine
// only" scope: open items of the given type (typeQualifier is "is:issue" or
// "is:pr") in the repo that involve the current user (involves:@me covers
// authored, assigned, mentioned, or commented-on). Scoping each tab to its own
// type is what gives each an accurate count and an independent pagination cursor.
func mineSearchQuery(repo repoInfo, typeQualifier string) string {
	return fmt.Sprintf("repo:%s/%s is:open %s involves:@me", repo.Owner, repo.Name, typeQualifier)
}

// mineSearchItems converts a type-scoped mine search connection's nodes into list
// rows, tagging each with type_ ("issue" or "pr") so the row/detail code matches
// the repo-connection path.
func mineSearchItems(nodes []mineSearchNode, type_ string) []list.Item {
	items := make([]list.Item, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, item{number: n.Number, title: n.Title, body: n.Body, author: n.Author.Login, type_: type_})
	}
	return items
}

// fetchMineItems returns the cmd that loads the first page of the "mine only"
// scope. It runs two type-scoped `search(type: ISSUE, query: "...involves:@me")`
// connections in a single request — one is:issue for the issues tab and one
// is:pr for the PRs tab — so each tab gets its own accurate count and its own
// pagination cursor (mirroring the separate issues/pullRequests connections of
// the repo path). A single merged search could not: it shares one count and one
// cursor across both types, so a page skewed to one type leaves the other tab
// wrongly empty. It returns the same dataMsg shape as fetchIssuesAndPRs, so the
// model/list code is identical across scopes. It is a package var (like
// fetchIssuesAndPRs) so tests can swap in a hermetic data source.
var fetchMineItems = func(conn *githubConn) tea.Cmd {
	return func() tea.Msg {
		client, repo, err := conn.resolve()
		if err != nil {
			return errMsg{err}
		}

		query := `
			query($issueSearch: String!, $prSearch: String!, $first: Int!) {
				rateLimit { remaining resetAt }
				issues: search(type: ISSUE, query: $issueSearch, first: $first) {
					issueCount
					pageInfo { endCursor hasNextPage }
					nodes {
						... on Issue {
							number
							title
							body
							author { login }
						}
					}
				}
				prs: search(type: ISSUE, query: $prSearch, first: $first) {
					issueCount
					pageInfo { endCursor hasNextPage }
					nodes {
						... on PullRequest {
							number
							title
							body
							author { login }
						}
					}
				}
			}
		`
		variables := map[string]interface{}{
			"issueSearch": mineSearchQuery(repo, "is:issue"),
			"prSearch":    mineSearchQuery(repo, "is:pr"),
			"first":       listPageSize,
		}

		type searchConn struct {
			IssueCount int              `json:"issueCount"`
			PageInfo   pageInfo         `json:"pageInfo"`
			Nodes      []mineSearchNode `json:"nodes"`
		}
		type response struct {
			RateLimit rateLimitNode `json:"rateLimit"`
			Issues    searchConn    `json:"issues"`
			PRs       searchConn    `json:"prs"`
		}

		var resp response
		if err := client.Do(query, variables, &resp); err != nil {
			return errMsg{err}
		}
		conn.setRateLimit(resp.RateLimit)

		// Each tab is backed by its own type-scoped search: its own count and its
		// own cursor (no merged total/cursor shared across the two tabs).
		return dataMsg{
			issues:           mineSearchItems(resp.Issues.Nodes, "issue"),
			prs:              mineSearchItems(resp.PRs.Nodes, "pr"),
			issueTotal:       resp.Issues.IssueCount,
			prTotal:          resp.PRs.IssueCount,
			issueEndCursor:   resp.Issues.PageInfo.EndCursor,
			issueHasNextPage: resp.Issues.PageInfo.HasNextPage,
			prEndCursor:      resp.PRs.PageInfo.EndCursor,
			prHasNextPage:    resp.PRs.PageInfo.HasNextPage,
		}
	}
}

// fetchMoreMineItems returns the cmd that loads the next page of the "mine only"
// scope for a single tab, starting after that tab's cursor. Each tab has its own
// type-scoped search (is:issue or is:pr) and its own cursor, so a load-more
// advances only the tab that triggered it — the returned moreDataMsg appends to
// that one tab, exactly like the repo-connection path (fetchMoreItems). It is a
// package var so tests can stub it. The tab argument selects the type qualifier
// and is carried back on the message for the in-flight guard.
var fetchMoreMineItems = func(conn *githubConn, t tab, after string) tea.Cmd {
	return func() tea.Msg {
		client, repo, err := conn.resolve()
		if err != nil {
			return moreDataMsg{tab: t, err: err}
		}

		typeQualifier, type_ := "is:issue", "issue"
		if t == tabPRs {
			typeQualifier, type_ = "is:pr", "pr"
		}

		query := `
			query($search: String!, $first: Int!, $after: String!) {
				rateLimit { remaining resetAt }
				search(type: ISSUE, query: $search, first: $first, after: $after) {
					pageInfo { endCursor hasNextPage }
					nodes {
						... on Issue {
							number
							title
							body
							author { login }
						}
						... on PullRequest {
							number
							title
							body
							author { login }
						}
					}
				}
			}
		`
		variables := map[string]interface{}{
			"search": mineSearchQuery(repo, typeQualifier),
			"first":  listPageSize,
			"after":  after,
		}

		type response struct {
			RateLimit rateLimitNode `json:"rateLimit"`
			Search    struct {
				PageInfo pageInfo         `json:"pageInfo"`
				Nodes    []mineSearchNode `json:"nodes"`
			} `json:"search"`
		}

		var resp response
		if err := client.Do(query, variables, &resp); err != nil {
			return moreDataMsg{tab: t, err: err}
		}
		conn.setRateLimit(resp.RateLimit)

		return moreDataMsg{
			tab:         t,
			items:       mineSearchItems(resp.Search.Nodes, type_),
			endCursor:   resp.Search.PageInfo.EndCursor,
			hasNextPage: resp.Search.PageInfo.HasNextPage,
		}
	}
}

// fetchVisibleItems returns the cmd that re-fetches the current state (title +
// open/closed) of a specific set of already-loaded rows by number, in one
// aliased round-trip (n0, n1, ... mirroring fetchLabels). It backs the deep
// auto-refresh: refreshing only the on-screen window keeps the cost bounded by
// screen height (not list depth) and avoids the reorder/clobber a full refetch
// would cause. It is a package var so tests can stub it.
var fetchVisibleItems = func(conn *githubConn, t tab, numbers []int) tea.Cmd {
	return func() tea.Msg {
		if len(numbers) == 0 {
			return visibleRefreshMsg{tab: t, items: map[int]refreshedItem{}}
		}
		client, repo, err := conn.resolve()
		if err != nil {
			return visibleRefreshMsg{tab: t, err: err}
		}

		field := "issue"
		if t == tabPRs {
			field = "pullRequest"
		}
		var b strings.Builder
		b.WriteString("query($owner: String!, $repo: String!) {\n")
		b.WriteString("\trepository(owner: $owner, name: $repo) {\n")
		for i, n := range numbers {
			fmt.Fprintf(&b, "\t\tn%d: %s(number: %d) { number title state }\n", i, field, n)
		}
		b.WriteString("\t}\n")
		b.WriteString("\trateLimit { remaining resetAt }\n")
		b.WriteString("}")
		variables := map[string]interface{}{
			"owner": repo.Owner,
			"repo":  repo.Name,
		}

		type aliased struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
		}
		// The aliased fields share one shape; decode the repository object into an
		// alias->node map. A null entry (deleted item) is skipped.
		var resp struct {
			RateLimit  rateLimitNode       `json:"rateLimit"`
			Repository map[string]*aliased `json:"repository"`
		}
		if err := client.Do(b.String(), variables, &resp); err != nil {
			return visibleRefreshMsg{tab: t, err: err}
		}
		conn.setRateLimit(resp.RateLimit)

		items := make(map[int]refreshedItem, len(resp.Repository))
		for _, n := range resp.Repository {
			if n == nil {
				continue
			}
			// state is OPEN for live items; CLOSED (issues/PRs) or MERGED (PRs)
			// otherwise — anything non-OPEN renders the "[closed]" prefix.
			items[n.Number] = refreshedItem{title: n.Title, closed: n.State != "OPEN"}
		}
		return visibleRefreshMsg{tab: t, items: items}
	}
}
