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
