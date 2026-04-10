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
	Username      string                    `json:"username"`
	Email         string                    `json:"email"`
	CommitteeUID  string                    `json:"committee_uid"`
	CommitteeName string                    `json:"committee_name"`
	ProjectUID    string                    `json:"project_uid"`
	ProjectSlug   string                    `json:"project_slug"`
	Role          CommitteeMemberRole       `json:"role"`
	Voting        CommitteeMemberVote       `json:"voting"`
	Organization  CommitteeMemberOrg        `json:"organization"`
}

// CommitteeMemberOrg is the nested organization object on a committee_member.
type CommitteeMemberOrg struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Website string `json:"website"`
}

// CommitteeMemberRole is the nested role object on a committee_member.
type CommitteeMemberRole struct {
	Name string `json:"name"`
}

// CommitteeMemberVote is the nested voting object on a committee_member.
type CommitteeMemberVote struct {
	Status string `json:"status"`
}


// ProjectData represents the data fields on a project_settings resource.
type ProjectData struct {
	UID      string          `json:"uid"`
	Writers  []ProjectPerson `json:"writers"`
	Auditors []ProjectPerson `json:"auditors"`
}

// ProjectPerson represents a person entry in a project's writers or auditors list.
type ProjectPerson struct {
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Username string `json:"username"`
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
