// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

import "encoding/json"

// PersonaRequest represents the incoming NATS request payload.
type PersonaRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

// PersonaResponse represents the NATS reply payload.
type PersonaResponse struct {
	Projects []Project    `json:"projects"`
	Error    *ErrorDetail `json:"error"`
}

// ErrorDetail carries a machine-readable code and human-readable message
// for hard failures surfaced in the top-level response error field.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Project represents a single project entry in the response.
type Project struct {
	ProjectUID  string      `json:"project_uid"`
	ProjectSlug string      `json:"project_slug"`
	Detections  []Detection `json:"detections"`
}

// Detection represents a single source detection for a project.
type Detection struct {
	Source string          `json:"source"`
	Extra  json.RawMessage `json:"extra,omitempty"`
}

// Detection source tokens.
const (
	SourceBoardMember       = "board_member"
	SourceExecutiveDirector = "executive_director"
	SourceCDPActivity       = "cdp_activity"
	SourceCDPRoles          = "cdp_roles"
	SourceWriterAuditor     = "writer_auditor"
	SourceCommitteeMember   = "committee_member"
	SourceMailingList       = "mailing_list"
	SourceMeetingAttendance = "meeting_attendance"
)

// BoardMemberExtra is the extra payload for a board_member detection.
type BoardMemberExtra struct {
	CommitteeUID       string `json:"committee_uid"`
	CommitteeName      string `json:"committee_name"`
	CommitteeMemberUID string `json:"committee_member_uid"`
	Role               string `json:"role"`
	VotingStatus       string `json:"voting_status"`
}

// CommitteeMemberExtra is the extra payload for a committee_member detection (Source 4).
type CommitteeMemberExtra struct {
	CommitteeUID       string `json:"committee_uid"`
	CommitteeName      string `json:"committee_name"`
	CommitteeMemberUID string `json:"committee_member_uid"`
	Role               string `json:"role"`
}

// CDPRolesExtra is the extra payload for a cdp_roles detection.
// Roles are passed through as-is from CDP; the UI interprets them.
type CDPRolesExtra struct {
	ContributionCount int                    `json:"contributionCount"`
	Roles             []CDPRolesExtraRole    `json:"roles"`
}

// CDPRolesExtraRole mirrors the role shape from the CDP project-affiliations response.
type CDPRolesExtraRole struct {
	ID          string  `json:"id"`
	Role        string  `json:"role"`
	StartDate   string  `json:"startDate"`
	EndDate     *string `json:"endDate"`
	RepoURL     string  `json:"repoUrl"`
	RepoFileURL string  `json:"repoFileUrl"`
}

// MergeProjects merges detections from src into dst, de-duplicating by ProjectUID.
// Projects that exist in dst get additional detections appended; new projects
// are added to the end.
func MergeProjects(dst, src []Project) []Project {
	idx := make(map[string]int, len(dst))
	for i, p := range dst {
		idx[p.ProjectUID] = i
	}
	for _, p := range src {
		if i, ok := idx[p.ProjectUID]; ok {
			dst[i].Detections = append(dst[i].Detections, p.Detections...)
		} else {
			idx[p.ProjectUID] = len(dst)
			dst = append(dst, p)
		}
	}
	return dst
}
