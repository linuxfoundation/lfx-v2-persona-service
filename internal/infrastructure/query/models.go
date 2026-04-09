// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package query

import "encoding/json"

// ResourceResponse is the top-level response from GET /query/resources.
type ResourceResponse struct {
	Resources []Resource `json:"resources"`
}

// Resource represents a single resource returned by the Query Service.
type Resource struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Slug string          `json:"slug"`
	Data json.RawMessage `json:"data"`
	Tags []string        `json:"tags"`
}

// CommitteeMemberData represents the data fields on a committee_member resource.
type CommitteeMemberData struct {
	Username      string              `json:"username"`
	Email         string              `json:"email"`
	CommitteeUID  string              `json:"committee_uid"`
	CommitteeName string              `json:"committee_name"`
	Role          CommitteeMemberRole `json:"role"`
	Voting        CommitteeMemberVote `json:"voting"`
}

// CommitteeMemberRole is the nested role object on a committee_member.
type CommitteeMemberRole struct {
	Name string `json:"name"`
}

// CommitteeMemberVote is the nested voting object on a committee_member.
type CommitteeMemberVote struct {
	Status string `json:"status"`
}

// CommitteeData represents the data fields on a committee resource.
type CommitteeData struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	ProjectUID  string `json:"project_uid"`
	ProjectSlug string `json:"project_slug"`
	ProjectName string `json:"project_name"`
}

// ProjectData represents the data fields on a project resource.
type ProjectData struct {
	Slug     string   `json:"slug"`
	Writers  []string `json:"writers"`
	Auditors []string `json:"auditors"`
}

// GroupsIOMemberData represents the data fields on a groupsio_member resource.
type GroupsIOMemberData struct {
	Username       string `json:"username"`
	Email          string `json:"email"`
	MailingListUID string `json:"mailing_list_uid"`
	ProjectUID     string `json:"project_uid"`
	ProjectSlug    string `json:"project_slug"`
}

// MeetingParticipantData represents the data fields on a v1_past_meeting_participant resource.
type MeetingParticipantData struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	IsAttended  bool   `json:"is_attended"`
	IsInvited   bool   `json:"is_invited"`
	ProjectUID  string `json:"project_uid"`
	ProjectSlug string `json:"project_slug"`
}
