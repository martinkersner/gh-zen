package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeGraphQLClient is an injectable graphQLClient for the fetch-layer tests. It
// records the last query/variables, unmarshals a canned JSON payload into the
// response struct (exactly as the real client does, so it's robust to the
// fetch layer's private response types), and can return a configured error to
// drive the error branches.
//
// The recorder fields are guarded by mu so a single fake can be shared across
// concurrent Do calls without racing. The e2e tests open detail views whose
// cmdFetchBody runs in a background goroutine alongside the auto-refresh tick,
// so Do can be invoked concurrently; without the mutex go test -race would flag
// a data race on the gotQuery/gotVars writes.
type fakeGraphQLClient struct {
	err      error
	respJSON string

	mu       sync.Mutex
	gotQuery string
	gotVars  map[string]interface{}
}

func (f *fakeGraphQLClient) Do(query string, variables map[string]interface{}, response interface{}) error {
	f.mu.Lock()
	f.gotQuery = query
	f.gotVars = variables
	f.mu.Unlock()
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

// lastQuery and lastVars read the recorded query/variables under the mutex so
// assertions are safe even if a concurrent Do is in flight.
func (f *fakeGraphQLClient) lastQuery() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotQuery
}

func (f *fakeGraphQLClient) lastVars() map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotVars
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
		respJSON: `{"repository":{
			"issue":{"body":"issue body content","author":{"login":"octocat"},"labels":{"nodes":[
				{"name":"bug","color":"d73a4a"},
				{"name":"good first issue","color":"7057ff"}
			]},"comments":{"totalCount":2,"nodes":[
				{"author":{"login":"alice"},"body":"first comment"},
				{"author":{"login":"bob"},"body":"second comment"}
			]}},
			"pullRequest":{"body":"SHOULD NOT BE USED","comments":{"nodes":[]}}
		}}`,
	}
	body, comments, total, labels, author, err := fetchBody(fake, testRepo(), 42, false)
	if err != nil {
		t.Fatalf("fetchBody returned error: %v", err)
	}
	if body != "issue body content" {
		t.Errorf("body = %q, want %q", body, "issue body content")
	}
	if author != "octocat" {
		t.Errorf("author = %q, want %q", author, "octocat")
	}
	if !strings.Contains(fake.gotQuery, "author { login }") {
		t.Errorf("query did not request the author:\n%s", fake.gotQuery)
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(comments))
	}
	if total != 2 {
		t.Errorf("totalCount = %d, want 2", total)
	}
	if len(labels) != 2 {
		t.Fatalf("got %d labels, want 2", len(labels))
	}
	if labels[0].name != "bug" || labels[0].color != "d73a4a" {
		t.Errorf("label[0] = %+v, want {bug d73a4a}", labels[0])
	}
	if labels[1].name != "good first issue" || labels[1].color != "7057ff" {
		t.Errorf("label[1] = %+v, want {good first issue 7057ff}", labels[1])
	}
	if !strings.Contains(fake.gotQuery, "labels(first: $labels)") {
		t.Errorf("query did not request labels:\n%s", fake.gotQuery)
	}
	if !strings.Contains(fake.gotQuery, "totalCount") {
		t.Errorf("query did not request totalCount:\n%s", fake.gotQuery)
	}
	if comments[0].author != "alice" || comments[0].body != "first comment" {
		t.Errorf("comment[0] = %+v", comments[0])
	}
	if comments[1].author != "bob" || comments[1].body != "second comment" {
		t.Errorf("comment[1] = %+v", comments[1])
	}
	// Verify the issue field (not pullRequest) was queried and variables wired.
	gotVars := fake.lastVars()
	gotQuery := fake.lastQuery()
	if got := gotVars["number"]; got != 42 {
		t.Errorf("number var = %v, want 42", got)
	}
	if got := gotVars["comments"]; got != commentsFetchLimit {
		t.Errorf("comments var = %v, want %d", got, commentsFetchLimit)
	}
	if got := gotVars["labels"]; got != labelsFetchLimit {
		t.Errorf("labels var = %v, want %d", got, labelsFetchLimit)
	}
	if !strings.Contains(gotQuery, "comments(first: $comments)") {
		t.Errorf("query did not request comments:\n%s", gotQuery)
	}
	if got := gotVars["owner"]; got != testRepoOwner {
		t.Errorf("owner var = %v, want %q", got, testRepoOwner)
	}
	if got := gotVars["repo"]; got != testRepoName {
		t.Errorf("repo var = %v, want %q", got, testRepoName)
	}
	if !strings.Contains(gotQuery, "issue(number: $number)") {
		t.Errorf("query did not target the issue field:\n%s", gotQuery)
	}
}

func TestFetchBodyPRSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"issue":{"body":"SHOULD NOT BE USED","comments":{"nodes":[]}},
			"pullRequest":{"body":"pr body content","author":{"login":"hubot"},"comments":{"totalCount":60,"nodes":[
				{"author":{"login":"carol"},"body":"pr comment"}
			]}}
		}}`,
	}
	body, comments, total, labels, author, err := fetchBody(fake, testRepo(), 7, true)
	if err != nil {
		t.Fatalf("fetchBody returned error: %v", err)
	}
	if body != "pr body content" {
		t.Errorf("body = %q, want %q", body, "pr body content")
	}
	if author != "hubot" {
		t.Errorf("author = %q, want %q", author, "hubot")
	}
	if len(comments) != 1 || comments[0].author != "carol" || comments[0].body != "pr comment" {
		t.Errorf("comments = %+v", comments)
	}
	// The PR payload carries no labels node, so labels comes back empty (the
	// "no labels" case): nil, not a panic.
	if len(labels) != 0 {
		t.Errorf("labels = %+v, want none", labels)
	}
	// totalCount can exceed the returned node count when the thread is longer
	// than commentsFetchLimit; fetchBody surfaces the true total.
	if total != 60 {
		t.Errorf("totalCount = %d, want 60", total)
	}
	if q := fake.lastQuery(); !strings.Contains(q, "pullRequest(number: $number)") {
		t.Errorf("query did not target the pullRequest field:\n%s", q)
	}
}

// githubConn.resolve surfaces a client-construction failure (and doesn't cache
// it, so a later call retries — see TestGithubConnResolveMemoizes).
func TestGithubConnResolveClientError(t *testing.T) {
	wantErr := errors.New("no auth token")
	withFakeGitHub(t, nil, wantErr, testRepo(), nil)

	_, _, err := (&githubConn{}).resolve()
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// githubConn.resolve surfaces a repo-resolution failure.
func TestGithubConnResolveRepoError(t *testing.T) {
	wantErr := errors.New("not a git repo")
	withFakeGitHub(t, &fakeGraphQLClient{}, nil, repoInfo{}, wantErr)

	_, _, err := (&githubConn{}).resolve()
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestFetchBodyQueryError(t *testing.T) {
	wantErr := errors.New("graphql: rate limited")
	fake := &fakeGraphQLClient{err: wantErr}

	_, _, _, _, _, err := fetchBody(fake, testRepo(), 1, false)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// --- fetchIssuesAndPRs ---

func TestFetchIssuesAndPRsSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"issues":{"totalCount":42,"nodes":[{"number":11,"title":"issue one","body":"ibody"}]},
			"pullRequests":{"totalCount":7,"nodes":[{"number":21,"title":"pr one","body":"pbody"}]}
		}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchIssuesAndPRs(&githubConn{})()
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
	if data.issueTotal != 42 || data.prTotal != 7 {
		t.Errorf("totals = issues %d, prs %d; want 42, 7", data.issueTotal, data.prTotal)
	}
	gotVars := fake.lastVars()
	if got := gotVars["owner"]; got != testRepoOwner {
		t.Errorf("owner var = %v, want %q", got, testRepoOwner)
	}
	if got := gotVars["repo"]; got != testRepoName {
		t.Errorf("repo var = %v, want %q", got, testRepoName)
	}
}

func TestFetchMoreItemsIssues(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"issues":{"pageInfo":{"endCursor":"C2","hasNextPage":true},"nodes":[{"number":99,"title":"page two","body":"b2"}]}
		}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchMoreItems(&githubConn{}, tabIssues, "C1")()
	more, ok := msg.(moreDataMsg)
	if !ok {
		t.Fatalf("expected moreDataMsg, got %T (%v)", msg, msg)
	}
	if more.tab != tabIssues || more.endCursor != "C2" || !more.hasNextPage {
		t.Errorf("moreDataMsg = %+v", more)
	}
	if len(more.items) != 1 {
		t.Fatalf("got %d items, want 1", len(more.items))
	}
	got := more.items[0].(item)
	if got.number != 99 || got.title != "page two" || got.type_ != "issue" {
		t.Errorf("item = %+v", got)
	}
	if after := fake.lastVars()["after"]; after != "C1" {
		t.Errorf("after var = %v, want C1", after)
	}
}

func TestFetchMoreItemsPRsError(t *testing.T) {
	wantErr := errors.New("rate limited")
	withFakeGitHub(t, &fakeGraphQLClient{err: wantErr}, nil, testRepo(), nil)

	msg := fetchMoreItems(&githubConn{}, tabPRs, "C1")()
	more, ok := msg.(moreDataMsg)
	if !ok {
		t.Fatalf("expected moreDataMsg, got %T", msg)
	}
	if more.tab != tabPRs || !errors.Is(more.err, wantErr) {
		t.Errorf("moreDataMsg = %+v, want tab=PRs err=%v", more, wantErr)
	}
}

func TestFetchVisibleItems(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"n0":{"number":1,"title":"fresh one","state":"OPEN"},
			"n1":{"number":2,"title":"fresh two","state":"CLOSED"},
			"n2":null
		}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchVisibleItems(&githubConn{}, tabIssues, []int{1, 2, 3})()
	vr, ok := msg.(visibleRefreshMsg)
	if !ok {
		t.Fatalf("expected visibleRefreshMsg, got %T (%v)", msg, msg)
	}
	if vr.tab != tabIssues {
		t.Errorf("tab = %v, want issues", vr.tab)
	}
	if len(vr.items) != 2 {
		t.Fatalf("got %d items, want 2 (null skipped)", len(vr.items))
	}
	if got := vr.items[1]; got.title != "fresh one" || got.closed {
		t.Errorf("item 1 = %+v, want {fresh one, open}", got)
	}
	if got := vr.items[2]; got.title != "fresh two" || !got.closed {
		t.Errorf("item 2 = %+v, want {fresh two, closed}", got)
	}
	// The aliased query must select the issue field for the issues tab.
	if q := fake.lastQuery(); !strings.Contains(q, "issue(number: 1)") {
		t.Errorf("query missing aliased issue selection: %q", q)
	}
}

func TestFetchVisibleItemsEmpty(t *testing.T) {
	// No numbers → no round-trip, empty result.
	fake := &fakeGraphQLClient{err: errors.New("should not be called")}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchVisibleItems(&githubConn{}, tabPRs, nil)()
	vr, ok := msg.(visibleRefreshMsg)
	if !ok {
		t.Fatalf("expected visibleRefreshMsg, got %T", msg)
	}
	if vr.err != nil || len(vr.items) != 0 {
		t.Errorf("empty fetch = %+v, want no err / no items", vr)
	}
}

func TestFetchIssuesAndPRsClientError(t *testing.T) {
	wantErr := errors.New("no auth token")
	withFakeGitHub(t, nil, wantErr, testRepo(), nil)

	msg := fetchIssuesAndPRs(&githubConn{})()
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

	msg := fetchIssuesAndPRs(&githubConn{})()
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

	msg := fetchIssuesAndPRs(&githubConn{})()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(em.err, wantErr) {
		t.Errorf("err = %v, want %v", em.err, wantErr)
	}
}

// --- fetchMineItems (involves:@me search scope) ---

// Two type-scoped search connections (is:issue, is:pr) back the two tabs, each
// with its own count and cursor — no merged total/cursor shared across tabs.
func TestFetchMineItemsSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{
			"issues":{
				"issueCount":5,
				"pageInfo":{"endCursor":"IC","hasNextPage":true},
				"nodes":[{"number":11,"title":"my issue","body":"ib","author":{"login":"me"}}]
			},
			"prs":{
				"issueCount":3,
				"pageInfo":{"endCursor":"PC","hasNextPage":false},
				"nodes":[{"number":21,"title":"my pr","body":"pb","author":{"login":"me"}}]
			}
		}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchMineItems(&githubConn{})()
	data, ok := msg.(dataMsg)
	if !ok {
		t.Fatalf("expected dataMsg, got %T (%v)", msg, msg)
	}
	if len(data.issues) != 1 || len(data.prs) != 1 {
		t.Fatalf("got %d issues, %d prs; want 1 each", len(data.issues), len(data.prs))
	}
	gotIssue := data.issues[0].(item)
	if gotIssue.number != 11 || gotIssue.title != "my issue" || gotIssue.type_ != "issue" || gotIssue.author != "me" {
		t.Errorf("issue item = %+v", gotIssue)
	}
	gotPR := data.prs[0].(item)
	if gotPR.number != 21 || gotPR.title != "my pr" || gotPR.type_ != "pr" {
		t.Errorf("pr item = %+v", gotPR)
	}
	// Each tab carries its own scoped count.
	if data.issueTotal != 5 || data.prTotal != 3 {
		t.Errorf("totals = issues %d, prs %d; want 5, 3", data.issueTotal, data.prTotal)
	}
	// Each tab carries its own cursor/hasNext (issues paginate, PRs don't).
	if data.issueEndCursor != "IC" || !data.issueHasNextPage {
		t.Errorf("issue pagination = %q/%v; want IC/true", data.issueEndCursor, data.issueHasNextPage)
	}
	if data.prEndCursor != "PC" || data.prHasNextPage {
		t.Errorf("pr pagination = %q/%v; want PC/false", data.prEndCursor, data.prHasNextPage)
	}
	// The query must run two type-scoped searches involving the current user.
	q := fake.lastQuery()
	if !strings.Contains(q, "issues: search(type: ISSUE") || !strings.Contains(q, "prs: search(type: ISSUE") {
		t.Errorf("query missing aliased type-scoped searches: %q", q)
	}
	if got := fake.lastVars()["issueSearch"]; got != "repo:octo/hello is:open is:issue involves:@me" {
		t.Errorf("issueSearch var = %v, want is:issue involves:@me query", got)
	}
	if got := fake.lastVars()["prSearch"]; got != "repo:octo/hello is:open is:pr involves:@me" {
		t.Errorf("prSearch var = %v, want is:pr involves:@me query", got)
	}
}

func TestFetchMineItemsClientError(t *testing.T) {
	wantErr := errors.New("no auth token")
	withFakeGitHub(t, nil, wantErr, testRepo(), nil)

	msg := fetchMineItems(&githubConn{})()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(em.err, wantErr) {
		t.Errorf("err = %v, want %v", em.err, wantErr)
	}
}

func TestFetchMineItemsQueryError(t *testing.T) {
	wantErr := errors.New("graphql: server error")
	withFakeGitHub(t, &fakeGraphQLClient{err: wantErr}, nil, testRepo(), nil)

	msg := fetchMineItems(&githubConn{})()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(em.err, wantErr) {
		t.Errorf("err = %v, want %v", em.err, wantErr)
	}
}

// A mine "load more" page belongs to the single tab that triggered it, fetched
// from that tab's type-scoped search; the message appends to that one tab.
func TestFetchMoreMineItems(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"search":{
			"pageInfo":{"endCursor":"MC2","hasNextPage":false},
			"nodes":[
				{"number":88,"title":"page two pr","body":"b","author":{"login":"me"}}
			]
		}}`,
	}
	withFakeGitHub(t, fake, nil, testRepo(), nil)

	msg := fetchMoreMineItems(&githubConn{}, tabPRs, "MC")()
	more, ok := msg.(moreDataMsg)
	if !ok {
		t.Fatalf("expected moreDataMsg, got %T (%v)", msg, msg)
	}
	if more.tab != tabPRs || more.endCursor != "MC2" || more.hasNextPage {
		t.Errorf("moreDataMsg = %+v", more)
	}
	if len(more.items) != 1 {
		t.Fatalf("got %d items; want 1", len(more.items))
	}
	if got := more.items[0].(item); got.number != 88 || got.type_ != "pr" {
		t.Errorf("pr item = %+v", got)
	}
	// The PRs tab load-more must use the is:pr type-scoped search and its cursor.
	if got := fake.lastVars()["search"]; got != "repo:octo/hello is:open is:pr involves:@me" {
		t.Errorf("search var = %v, want is:pr involves:@me query", got)
	}
	if after := fake.lastVars()["after"]; after != "MC" {
		t.Errorf("after var = %v, want MC", after)
	}
}

func TestFetchMoreMineItemsError(t *testing.T) {
	wantErr := errors.New("rate limited")
	withFakeGitHub(t, &fakeGraphQLClient{err: wantErr}, nil, testRepo(), nil)

	msg := fetchMoreMineItems(&githubConn{}, tabIssues, "MC")()
	more, ok := msg.(moreDataMsg)
	if !ok {
		t.Fatalf("expected moreDataMsg, got %T", msg)
	}
	if more.tab != tabIssues || !errors.Is(more.err, wantErr) {
		t.Errorf("moreDataMsg = %+v, want tab=issues err=%v", more, wantErr)
	}
}

// --- fetchLabels (batched visible-window prefetch) ---

// A mixed issue+PR batch builds one aliased query and maps each alias's labels
// back to its bodyCache key.
func TestFetchLabelsBatchedSuccess(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"n0":{"labels":{"nodes":[{"name":"bug","color":"d73a4a"}]}},
			"n1":{"labels":{"nodes":[{"name":"enhancement","color":"a2eeef"},{"name":"wip","color":"fbca04"}]}}
		}}`,
	}
	targets := []labelTarget{
		{key: "issue_12", number: 12, isPR: false},
		{key: "pr_7", number: 7, isPR: true},
	}
	got, err := fetchLabels(fake, testRepo(), targets)
	if err != nil {
		t.Fatalf("fetchLabels returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keyed results, want 2: %+v", len(got), got)
	}
	if l := got["issue_12"]; len(l) != 1 || l[0].name != "bug" || l[0].color != "d73a4a" {
		t.Errorf("issue_12 labels = %+v", l)
	}
	if l := got["pr_7"]; len(l) != 2 || l[0].name != "enhancement" || l[1].name != "wip" {
		t.Errorf("pr_7 labels = %+v", l)
	}

	// The query uses aliased issue/pullRequest fields capped at labelsFetchLimit.
	q := fake.lastQuery()
	if !strings.Contains(q, "n0: issue(number: 12)") {
		t.Errorf("query missing aliased issue field:\n%s", q)
	}
	if !strings.Contains(q, "n1: pullRequest(number: 7)") {
		t.Errorf("query missing aliased pullRequest field:\n%s", q)
	}
	if !strings.Contains(q, fmt.Sprintf("labels(first: %d)", labelsFetchLimit)) {
		t.Errorf("query did not cap labels at labelsFetchLimit:\n%s", q)
	}
	gotVars := fake.lastVars()
	if gotVars["owner"] != testRepoOwner || gotVars["repo"] != testRepoName {
		t.Errorf("owner/repo vars = %v/%v", gotVars["owner"], gotVars["repo"])
	}
}

// A target whose payload carries no labels is simply absent from the result map
// (not a nil/empty entry), so the cache-merge doesn't warm it with nothing.
func TestFetchLabelsOmitsEmpty(t *testing.T) {
	fake := &fakeGraphQLClient{
		respJSON: `{"repository":{
			"n0":{"labels":{"nodes":[]}},
			"n1":{"labels":{"nodes":[{"name":"bug","color":"d73a4a"}]}}
		}}`,
	}
	got, err := fetchLabels(fake, testRepo(), []labelTarget{
		{key: "issue_1", number: 1},
		{key: "issue_2", number: 2},
	})
	if err != nil {
		t.Fatalf("fetchLabels returned error: %v", err)
	}
	if _, ok := got["issue_1"]; ok {
		t.Errorf("issue_1 (no labels) should be absent from result, got %+v", got["issue_1"])
	}
	if l := got["issue_2"]; len(l) != 1 || l[0].name != "bug" {
		t.Errorf("issue_2 labels = %+v", l)
	}
}

// An empty target slice is a no-op: no client call, nil map, nil error, so the
// caller needn't special-case an empty on-screen window.
func TestFetchLabelsEmptyTargets(t *testing.T) {
	fake := &fakeGraphQLClient{respJSON: `{"repository":{}}`}

	got, err := fetchLabels(fake, testRepo(), nil)
	if err != nil {
		t.Fatalf("fetchLabels(nil) returned error: %v", err)
	}
	if got != nil {
		t.Errorf("fetchLabels(nil) = %+v, want nil", got)
	}
	if fake.lastQuery() != "" {
		t.Errorf("fetchLabels(nil) issued a query: %q", fake.lastQuery())
	}
}

// githubConn.resolve caches a successful resolution: the client/repo seams run
// once and every later call returns the memoized pair (so per-session fetches
// don't re-resolve the git remote). A prior failure is not cached.
func TestGithubConnResolveMemoizes(t *testing.T) {
	clientCalls, repoCalls := 0, 0
	origClient := newGraphQLClient
	origRepo := currentRepo
	newGraphQLClient = func() (graphQLClient, error) {
		clientCalls++
		return &fakeGraphQLClient{}, nil
	}
	currentRepo = func() (repoInfo, error) {
		repoCalls++
		return testRepo(), nil
	}
	t.Cleanup(func() {
		newGraphQLClient = origClient
		currentRepo = origRepo
	})

	conn := &githubConn{}
	for i := 0; i < 3; i++ {
		if _, _, err := conn.resolve(); err != nil {
			t.Fatalf("resolve #%d returned error: %v", i, err)
		}
	}
	if clientCalls != 1 || repoCalls != 1 {
		t.Errorf("seams resolved %d client / %d repo times across 3 calls, want 1/1", clientCalls, repoCalls)
	}
}

func TestFetchLabelsQueryError(t *testing.T) {
	wantErr := errors.New("graphql: rate limited")
	fake := &fakeGraphQLClient{err: wantErr}

	_, err := fetchLabels(fake, testRepo(), []labelTarget{{key: "issue_1", number: 1}})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
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
