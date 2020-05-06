package scmprovider

import (
	"bytes"
	"context"
	"io"

	"github.com/jenkins-x/go-scm/scm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ListReviews list the reviews
func (c *Client) ListReviews(owner, repo string, number int) ([]*scm.Review, error) {
	ctx := context.Background()
	fullName := c.repositoryName(owner, repo)
	var allReviews []*scm.Review
	var resp *scm.Response
	var reviews []*scm.Review
	var err error
	firstRun := false
	opts := scm.ListOptions{
		Page: 1,
	}
	for !firstRun || (resp != nil && opts.Page <= resp.Page.Last) {
		reviews, resp, err = c.client.Reviews.List(ctx, fullName, number, opts)
		if err != nil {
			return nil, err
		}
		firstRun = true
		allReviews = append(allReviews, reviews...)
		opts.Page++
	}
	return allReviews, nil
}

// RequestReview requests a review
func (c *Client) RequestReview(org, repo string, number int, logins []string) error {
	ctx := context.Background()
	fullName := c.repositoryName(org, repo)
	resp, err := c.client.PullRequests.RequestReview(ctx, fullName, number, logins)
	logrus.Warnf("SENT REQUEST REVIEW")
	if resp != nil {
		var b bytes.Buffer
		_, cperr := io.Copy(&b, resp.Body)
		if cperr != nil {
			logrus.WithError(cperr).Warnf("and something blew up copying the body")
		} else {
			logrus.Warnf("Resp body: %s", b.String())
			logrus.Warnf("Resp code: %d", resp.Status)
			for h, v := range resp.Header {
				logrus.Warnf("HEADER: %s, VALUE: %s", h, v)
			}
		}
	}
	if err != nil {
		logrus.WithError(err).Warnf("ERROR ON THE REQUEST REVIEW: %s", err.Error())
		return errors.Wrapf(err, "requesting review from %s", logins)
	}
	return nil
}

// UnrequestReview unrequest a review
func (c *Client) UnrequestReview(org, repo string, number int, logins []string) error {
	ctx := context.Background()
	fullName := c.repositoryName(org, repo)
	_, err := c.client.PullRequests.UnrequestReview(ctx, fullName, number, logins)
	if err != nil {
		return errors.Wrapf(err, "unrequesting review from %s", logins)
	}
	return nil
}
