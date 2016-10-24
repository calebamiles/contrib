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
	"k8s.io/contrib/mungegithub/features"
	"k8s.io/contrib/mungegithub/github"
	"k8s.io/contrib/mungegithub/mungers/mungerutil"

	"github.com/golang/glog"
	githubapi "github.com/google/go-github/github"
	"github.com/spf13/cobra"
)

// LGTMHandler will
// - apply LGTM label if reviewer has commented "/lgtm", or
// - remove LGTM label if reviewer has commented "/lgtm cancel"
type LGTMHandler struct{}

func init() {
	l := LGTMHandler{}
	RegisterMungerOrDie(l)
}

// Name is the name usable in --pr-mungers
func (LGTMHandler) Name() string { return "lgtm-handler" }

// RequiredFeatures is a slice of 'features' that must be provided
func (LGTMHandler) RequiredFeatures() []string { return []string{} }

// Initialize will initialize the munger
func (LGTMHandler) Initialize(config *github.Config, features *features.Features) error { return nil }

// EachLoop is called at the start of every munge loop
func (LGTMHandler) EachLoop() error { return nil }

// AddFlags will add any request flags to the cobra `cmd`
func (LGTMHandler) AddFlags(cmd *cobra.Command, config *github.Config) {}

// Munge is the workhorse the will actually make updates to the PR
func (h LGTMHandler) Munge(obj *github.MungeObject) {
	if !obj.IsPR() {
		return
	}

	reviewers := getReviewers(obj)
	if len(reviewers) == 0 {
		return
	}

	comments, err := getCommentsAfterLastModified(obj)
	if err != nil {
		glog.Errorf("unexpected error getting comments: %v", err)
		return
	}

	events, err := obj.GetEvents()
	if err != nil {
		glog.Errorf("unexpected error getting events: %v", err)
		return
	}

	if !obj.HasLabel(lgtmLabel) {
		h.addLGTMIfCommented(obj, comments, events, reviewers)
		return
	}
	h.removeLGTMIfCancelled(obj, comments, events, reviewers)
}

func (h *LGTMHandler) addLGTMIfCommented(obj *github.MungeObject, comments []*githubapi.IssueComment, events []*githubapi.IssueEvent, reviewers mungerutil.UserSet) {

	// Assumption: The comments should be sorted (by default from github api) from oldest to latest
	for i := len(comments) - 1; i >= 0; i-- {
		comment := comments[i]
		if !mungerutil.IsValidUser(comment.User) {
			continue
		}

		if !isApprovedToAlterLabels(comment.User, reviewers) {
			continue
		}

		fields := getFields(*comment.Body)
		if isCancelComment(fields) {
			// "/lgtm|approved cancel" if commented more recently than "/lgtm"
			return
		}

		if !isMergeComment(fields) {
			continue
		}

		// check if someone manually removed a mergable after the `/<mergableCommand>` comment
		// and honor it.
		removeMerableLabelTime := manuallyRemoveMerableLabelTime(events) // Get the last time when the someone applied lgtm or approve manually.
		if removeMerableLabelTime != nil && removeMerableLabelTime.After(*comment.CreatedAt) {
			return
		}

		// TODO: support more complex policies for multiple reviewers.
		// See https://github.com/kubernetes/contrib/issues/1389#issuecomment-235161164
		glog.Infof("Adding lgtm and approve label. Reviewer (%s) LGTM|approve", *comment.User.Login)
		obj.AddLabels([]string{lgtmLabel, approveLabel})
		return
	}
}

func (h *LGTMHandler) removeLGTMIfCancelled(obj *github.MungeObject, comments []*githubapi.IssueComment, events []*githubapi.IssueEvent, reviewers mungerutil.UserSet) {
	for i := len(comments) - 1; i >= 0; i-- {
		comment := comments[i]
		if !mungerutil.IsValidUser(comment.User) {
			continue
		}

		if !isApprovedToAlterLabels(comment.User, reviewers) {
			continue
		}

		fields := getFields(*comment.Body)
		if isMergeComment(fields) {
			// "/lgtm" if commented more recently than "/lgtm cancel"
			return
		}

		if !isCancelComment(fields) {
			continue
		}

		// check if someone manually added the lgtm label after the `/lgtm cancel` comment
		// and honor it.
		addLGTMTime := manuallyAddMergableLabelTime(events) // Get time when the last (unlabeled, mergableCommand) event occurred.
		if addLGTMTime != nil && addLGTMTime.After(*comment.CreatedAt) {
			return
		}

		glog.Infof("Removing lgtm label. Reviewer (%s) cancelled", *comment.User.Login)
		obj.RemoveLabel(lgtmLabel) //TODO create RemoveLabels() to remove multiple labels
		return
	}
}
