package scmprovider

import (
	"context"

	"github.com/jenkins-x/go-scm/scm"
)

// MergeDetails optional extra parameters
type MergeDetails struct {
	SHA           string
	MergeMethod   string
	CommitTitle   string
	CommitMessage string
}

// GetPullRequest returns the pull request
func (c *Client) GetPullRequest(owner, repo string, number int) (*scm.PullRequest, error) {
	ctx := context.Background()
	fullName := c.repositoryName(owner, repo)
	pr, _, err := c.client.PullRequests.Find(ctx, fullName, number)
	return pr, err
}

// ListAllPullRequestsForFullNameRepo lists all pull requests in a full-name repository
func (c *Client) ListAllPullRequestsForFullNameRepo(fullName string, opts scm.PullRequestListOptions) ([]*scm.PullRequest, error) {
	ctx := context.Background()
	var allPRs []*scm.PullRequest
	var resp *scm.Response
	var pagePRs []*scm.PullRequest
	var err error
	for resp == nil || opts.Page <= resp.Page.Last {
		pagePRs, resp, err = c.client.PullRequests.List(ctx, fullName, opts)
		if err != nil {
			return nil, err
		}
		allPRs = append(allPRs, pagePRs...)
		opts.Page++
	}
	return allPRs, nil
}

// ListPullRequestComments list pull request comments
func (c *Client) ListPullRequestComments(owner, repo string, number int) ([]*scm.Comment, error) {
	ctx := context.Background()
	fullName := c.repositoryName(owner, repo)
	pr, _, err := c.client.PullRequests.ListComments(ctx, fullName, number, c.createListOptions())
	return pr, err
}

// GetPullRequestChanges returns the changes in a pull request
func (c *Client) GetPullRequestChanges(org, repo string, number int) ([]*scm.Change, error) {
	ctx := context.Background()
	fullName := c.repositoryName(org, repo)
	changes, _, err := c.client.PullRequests.ListChanges(ctx, fullName, number, c.createListOptions())
	return changes, err
}

// Merge reopens a pull request
func (c *Client) Merge(owner, repo string, number int, details MergeDetails) error {
	ctx := context.Background()
	fullName := c.repositoryName(owner, repo)
	mergeOptions := &scm.PullRequestMergeOptions{
		CommitTitle: details.CommitTitle,
		SHA:         details.SHA,
		MergeMethod: details.MergeMethod,
	}
	_, err := c.client.PullRequests.Merge(ctx, fullName, number, mergeOptions)
	return err
}

// ModifiedHeadError happens when github refuses to merge a PR because the PR changed.
type ModifiedHeadError string

func (e ModifiedHeadError) Error() string { return string(e) }

// UnmergablePRError happens when github refuses to merge a PR for other reasons (merge confclit).
type UnmergablePRError string

func (e UnmergablePRError) Error() string { return string(e) }

// UnmergablePRBaseChangedError happens when github refuses merging a PR because the base changed.
type UnmergablePRBaseChangedError string

func (e UnmergablePRBaseChangedError) Error() string { return string(e) }

// UnauthorizedToPushError happens when client is not allowed to push to github.
type UnauthorizedToPushError string

func (e UnauthorizedToPushError) Error() string { return string(e) }

// MergeCommitsForbiddenError happens when the repo disallows the merge strategy configured for the repo in Keeper.
type MergeCommitsForbiddenError string

func (e MergeCommitsForbiddenError) Error() string { return string(e) }

// ReopenPR reopens a pull request
func (c *Client) ReopenPR(owner, repo string, number int) error {
	panic("implement me")
}

// ClosePR closes a pull request
func (c *Client) ClosePR(owner, repo string, number int) error {
	panic("implement me")
}
