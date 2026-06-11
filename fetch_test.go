package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeGraphQLClient is an injectable graphQLClient for the fetch-layer tests. It
// records the last query/variables, unmarshals a canned JSON payload into the
// response struct (exactly as the real client does, so it's robust to the
// fetch layer's private response types), and can return a configured error to
// drive the error branches.
type fakeGraphQLClient struct {
	err      error
	respJSON string
	gotQuery string
	gotVars  map[string]interface{}
}

func (f *fakeGraphQLClient) Do(query string, variables map[string]interface{}, response interface{}) error {
	f.gotQuery = query
	f.gotVars = variables
	if f.err != nil {
		return f.err
	}
	if f.respJSON != "" {
		if err := json.Unmarshal([]byte(f.respJSON), response); err != nil {
			return err
		}
	}
	return nil
}

// withFakeGitHub swaps the injection seams (newGraphQLClient/currentRepo) for
// fakes for the duration of a test, restoring the originals on cleanup. A
// non-nil clientErr makes newGraphQLClient itself fail (the
// client-construction error branch); a non-nil repoErr makes currentRepo fail.
func withFakeGitHub(t *testing.T, client graphQLClient, clientErr error, repo repoInfo, repoErr error) {
	t.Helper()
	origClient := newGraphQLClient
	origRepo := currentRepo
	newGraphQLClient = func() (graphQLClient, error) {
		if clientErr != nil {
			return nil, clientErr
		}
		return client, nil
	}
	currentRepo = func() (repoInfo, error) {
		if repoErr != nil {
			return repoInfo{}, repoErr
		}
		return repo, nil
	}
	t.Cleanup(func() {
		newGraphQLClient = origClient
		currentRepo = origRepo
	})
}

const (
	testRepoOwner = "octo"
	testRepoName  = "hello"
)

func testRepo() repoInfo { return repoInfo{Owner: testRepoOwner, Name: testRepoName} }

// --- fetchBody ---

func TestFetchBodyIssueSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{"issue":{"body":"issue body content"},"pullRequest":{"body":"SHOULD NOT BE USED"}}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	body, err := fetchBody(42, false)
	if err != nil {
		t.Fatalf("fetchBody returned error: %v", err)
	}
	if body != "issue body content" {
		t.Errorf("body = %q, want %q", body, "issue body content")
	}
	// Verify the issue field (not pullRequest) was queried and variables wired.
	if got := fake.gotVars["number"]; got != 42 {
		t.Errorf("number var = %v, want 42", got)
	}
	if got := fake.gotVars["owner"]; got != testRepoOwner {
		t.Errorf("owner var = %v, want %q", got, testRepoOwner)
	}
	if got := fake.gotVars["repo"]; got != testRepoName {
		t.Errorf("repo var = %v, want %q", got, testRepoName)
	}
	if !strings.Contains(fake.gotQuery, "issue(number: $number)") {
		t.Errorf("query did not target the issue field:\n%s", fake.gotQuery)
	}
}

func TestFetchBodyPRSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{"issue":{"body":"SHOULD NOT BE USED"},"pullRequest":{"body":"pr body content"}}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	body, err := fetchBody(7, true)
	if err != nil {
		t.Fatalf("fetchBody returned error: %v", err)
	}
	if body != "pr body content" {
		t.Errorf("body = %q, want %q", body, "pr body content")
	}
	if !strings.Contains(fake.gotQuery, "pullRequest(number: $number)") {
		t.Errorf("query did not target the pullRequest field:\n%s", fake.gotQuery)
	}
}

func TestFetchBodyClientError(t *testing.T) {
	wantErr := errors.New("no auth token")
	withFakeGitHub(t, nil, wantErr, testRepo(), nil)

	_, err := fetchBody(1, false)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestFetchBodyRepoError(t *testing.T) {
	wantErr := errors.New("not a git repo")
	withFakeGitHub(t, &fakeGraphQLClient{}, nil, repoInfo{}, wantErr)

	_, err := fetchBody(1, false)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestFetchBodyQueryError(t *testing.T) {
	wantErr := errors.New("graphql: rate limited")
	fake := &fakeGraphQLClient{err: wantErr}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	_, err := fetchBody(1, false)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// --- fetchIssuesAndPRs ---

func TestFetchIssuesAndPRsSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"issues":{"nodes":[{"number":11,"title":"issue one","body":"ibody"}]},
			"pullRequests":{"nodes":[{"number":21,"title":"pr one","body":"pbody"}]}
		}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchIssuesAndPRs()()
	data, ok := msg.(dataMsg)
	if !ok {
		t.Fatalf("expected dataMsg, got %T (%v)", msg, msg)
	}
	if len(data.issues) != 1 || len(data.prs) != 1 {
		t.Fatalf("got %d issues, %d prs; want 1 each", len(data.issues), len(data.prs))
	}
	gotIssue := data.issues[0].(item)
	if gotIssue.number != 11 || gotIssue.title != "issue one" || gotIssue.body != "ibody" || gotIssue.type_ != "issue" {
		t.Errorf("issue item = %+v", gotIssue)
	}
	gotPR := data.prs[0].(item)
	if gotPR.number != 21 || gotPR.title != "pr one" || gotPR.body != "pbody" || gotPR.type_ != "pr" {
		t.Errorf("pr item = %+v", gotPR)
	}
	if got := fake.gotVars["owner"]; got != testRepoOwner {
		t.Errorf("owner var = %v, want %q", got, testRepoOwner)
	}
	if got := fake.gotVars["repo"]; got != testRepoName {
		t.Errorf("repo var = %v, want %q", got, testRepoName)
	}
}

func TestFetchIssuesAndPRsClientError(t *testing.T) {
	wantErr := errors.New("no auth token")
	withFakeGitHub(t, nil, wantErr, testRepo(), nil)

	msg := fetchIssuesAndPRs()()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(em.err, wantErr) {
		t.Errorf("err = %v, want %v", em.err, wantErr)
	}
}

func TestFetchIssuesAndPRsRepoError(t *testing.T) {
	wantErr := errors.New("not a git repo")
	withFakeGitHub(t, &fakeGraphQLClient{}, nil, repoInfo{}, wantErr)

	msg := fetchIssuesAndPRs()()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(em.err, wantErr) {
		t.Errorf("err = %v, want %v", em.err, wantErr)
	}
}

func TestFetchIssuesAndPRsQueryError(t *testing.T) {
	wantErr := errors.New("graphql: server error")
	fake := &fakeGraphQLClient{err: wantErr}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchIssuesAndPRs()()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(em.err, wantErr) {
		t.Errorf("err = %v, want %v", em.err, wantErr)
	}
}

// --- fetchDiff ---

func TestFetchDiffSuccess(t *testing.T) {
	withStubDiff(t, func(number int) (string, error) {
		if number != 9 {
			t.Errorf("fetchDiff called with %d, want 9", number)
		}
		return "diff --git a/x b/x\n+added\n", nil
	})

	diff, err := fetchDiff(9)
	if err != nil {
		t.Fatalf("fetchDiff returned error: %v", err)
	}
	if diff != "diff --git a/x b/x\n+added\n" {
		t.Errorf("diff = %q", diff)
	}
}

func TestFetchDiffError(t *testing.T) {
	wantErr := errors.New("no such pr")
	withStubDiff(t, func(int) (string, error) {
		return "", wantErr
	})

	_, err := fetchDiff(9)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
