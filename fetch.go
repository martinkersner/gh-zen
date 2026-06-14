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
	key  string
	body string
	err  error
}

type diffMsg struct {
	key  string
	diff string
	err  error
}

type errMsg struct {
	err error
}

// fetchBody pulls the current body for a single issue or PR from GitHub. It is a
// blocking call meant to run inside a tea.Cmd. Kept separate from the list fetch
// so the detail diff (fetchDiff) can grow independently.
func fetchBody(number int, isPR bool) (string, error) {
	client, err := newGraphQLClient()
	if err != nil {
		return "", err
	}
	repo, err := currentRepo()
	if err != nil {
		return "", err
	}

	field := "issue"
	if isPR {
		field = "pullRequest"
	}
	query := fmt.Sprintf(`
		query($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) {
				%s(number: $number) {
					body
				}
			}
		}
	`, field)
	variables := map[string]interface{}{
		"owner":  repo.Owner,
		"repo":   repo.Name,
		"number": number,
	}

	type response struct {
		Repository struct {
			Issue struct {
				Body string `json:"body"`
			} `json:"issue"`
			PullRequest struct {
				Body string `json:"body"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	var resp response
	if err := client.Do(query, variables, &resp); err != nil {
		return "", err
	}
	if isPR {
		return resp.Repository.PullRequest.Body, nil
	}
	return resp.Repository.Issue.Body, nil
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
