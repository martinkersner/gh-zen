package main

import (
	"fmt"
	"strconv"
	"strings"

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

type dataMsg struct {
	issues []list.Item
	prs    []list.Item
}

type bodyMsg struct {
	key    string
	body   string
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

type errMsg struct {
	err error
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
func fetchBody(number int, isPR bool) (string, []comment, int, []label, error) {
	client, err := newGraphQLClient()
	if err != nil {
		return "", nil, 0, nil, err
	}
	repo, err := currentRepo()
	if err != nil {
		return "", nil, 0, nil, err
	}

	field := "issue"
	if isPR {
		field = "pullRequest"
	}
	query := fmt.Sprintf(`
		query($owner: String!, $repo: String!, $number: Int!, $comments: Int!, $labels: Int!) {
			repository(owner: $owner, name: $repo) {
				%s(number: $number) {
					body
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
		return "", nil, 0, nil, err
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
	return d.Body, comments, d.Comments.TotalCount, labels, nil
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

// fetchIssuesAndPRs returns the cmd that loads the issue/PR lists from GitHub.
// It is a package var (like ghDiff) so tests can swap in a hermetic data source
// and drive the program offline without hitting the network.
var fetchIssuesAndPRs = func() tea.Cmd {
	return func() tea.Msg {
		client, err := newGraphQLClient()
		if err != nil {
			return errMsg{err}
		}

		repo, err := currentRepo()
		if err != nil {
			return errMsg{err}
		}

		query := `
			query($owner: String!, $repo: String!) {
				repository(owner: $owner, name: $repo) {
					issues(first: 50, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						nodes {
							number
							title
							body
						}
					}
					pullRequests(first: 50, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						nodes {
							number
							title
							body
						}
					}
				}
			}
		`
		variables := map[string]interface{}{
			"owner": repo.Owner,
			"repo":  repo.Name,
		}

		type node struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
		}
		type response struct {
			Repository struct {
				Issues struct {
					Nodes []node `json:"nodes"`
				} `json:"issues"`
				PullRequests struct {
					Nodes []node `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}

		var resp response
		if err := client.Do(query, variables, &resp); err != nil {
			return errMsg{err}
		}

		var issues, prs []list.Item
		for _, n := range resp.Repository.Issues.Nodes {
			issues = append(issues, item{number: n.Number, title: n.Title, body: n.Body, type_: "issue"})
		}
		for _, n := range resp.Repository.PullRequests.Nodes {
			prs = append(prs, item{number: n.Number, title: n.Title, body: n.Body, type_: "pr"})
		}

		return dataMsg{issues: issues, prs: prs}
	}
}
