/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mungers

import (
	"strings"
	"time"

	"k8s.io/contrib/mungegithub/github"
	e "k8s.io/contrib/mungegithub/mungers/matchers/event"
	"k8s.io/contrib/mungegithub/mungers/mungerutil"

	githubapi "github.com/google/go-github/github"
)

/*
  TODO: given how much code supports mergable command (lgtm|approved) label and command handling
  it probably makes sense to create a dedicated package
*/

const (
	approveLabel         = "approved"
	approveCommand       = "/approve"
	lgtmCommand          = "/lgtm" //this command is depricated, carry it until final removal
	cancelCommandModifer = "cancel"
)

func mergableLabelAddedTime(obj *github.MungeObject) *time.Time {
	lgtmTime := obj.LabelTime(lgtmLabel)
	approveTime := obj.LabelTime(approveLabel)

	switch {
	case lgtmTime == nil && approveTime == nil:
		return &time.Time{}
	case lgtmTime == nil:
		return approveTime
	case approveTime == nil:
		return lgtmTime
	}

	switch lgtmTime.After(*approveTime) {
	case true:
		return lgtmTime
	default:
		return approveTime
	}
}

func manuallyRemoveMerableLabelTime(events []*githubapi.IssueEvent) *time.Time {
	// match `approved` or `lgtm`
	approveOrLGTM := e.Or{e.LabelName(lgtmLabel), e.LabelName(approveLabel)}

	// Get the last time when the someone applied lgtm or approve manually.
	removeCommitAutoMergableTime := e.LastEvent(events, e.And{e.RemoveLabel{}, approveOrLGTM, e.HumanActor()}, nil)

	return removeCommitAutoMergableTime
}

func manuallyAddMergableLabelTime(events []*githubapi.IssueEvent) *time.Time {
	// match `approved` or `lgtm `
	approveOrLGTM := e.Or{e.LabelName(lgtmLabel), e.LabelName(approveLabel)}

	// Get time when the last (unlabeled, lgtm) event occurred.
	addLGTMTime := e.LastEvent(events, e.And{e.AddLabel{}, approveOrLGTM, e.HumanActor()}, nil)

	return addLGTMTime
}

func isMergeComment(fields []string) bool {
	// Note: later we'd probably move all the bot-command parsing code to its own package.
	if len(fields) != 1 {
		return false
	}

	return isMergeCommand(fields[0])
}

func isCancelComment(fields []string) bool {
	if len(fields) != 2 {
		return false
	}

	if strings.ToLower(fields[1]) != cancelCommandModifer {
		return false
	}

	return isMergeCommand(fields[0])
}

func getReviewers(obj *github.MungeObject) mungerutil.UserSet {
	// Note: assuming assignees are reviewers
	allAssignees := append(obj.Issue.Assignees, obj.Issue.Assignee)
	return mungerutil.GetUsers(allAssignees...)
}

func getCommentsAfterLastModified(obj *github.MungeObject) ([]*githubapi.IssueComment, error) {
	afterLastModified := func(opt *githubapi.IssueListCommentsOptions) *githubapi.IssueListCommentsOptions {
		// Only comments updated at or after this time are returned.
		// One possible case is that reviewer might "/lgtm" first, contributor updated PR, and reviewer updated "/lgtm".
		// This is still valid. We don't recommend user to update it.
		lastModified := *obj.LastModifiedTime()
		opt.Since = lastModified
		return opt
	}
	return obj.ListComments(afterLastModified)
}

func isReviewer(user *githubapi.User, reviewers mungerutil.UserSet) bool {
	return reviewers.Has(user)
}

// getFields will move to a different package where we do command
// parsing in the near future.
func getFields(commentBody string) []string {
	// remove the comment portion if present and read the command.
	cmd := strings.Split(commentBody, "//")[0]
	strings.TrimSpace(cmd)
	return strings.Fields(cmd)
}

func isApprovedToAlterLabels(user *githubapi.User, reviewers mungerutil.UserSet) bool {
	// TODO: An approver should be acceptable.
	// See https://github.com/kubernetes/contrib/pull/1428#discussion_r72563935
	return mungerutil.IsMungeBot(user) && reviewers.Has(user)
}

func isMergeCommand(cmd string) bool {
	switch strings.ToLower(cmd) {
	case lgtmCommand:
		return true
	case approveCommand:
		return true
	default:
		return false
	}
}
