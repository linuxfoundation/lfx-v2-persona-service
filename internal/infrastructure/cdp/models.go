// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package cdp

// ResolveRequest is the body for POST /v1/members/resolve.
type ResolveRequest struct {
	LFIDs  []string `json:"lfids"`
	Emails []string `json:"emails,omitempty"`
}

// ResolveResponse is the response from POST /v1/members/resolve.
type ResolveResponse struct {
	MemberID string `json:"memberId"`
}

// ProjectAffiliationsResponse wraps the list returned by GET /v1/members/{id}/project-affiliations.
type ProjectAffiliationsResponse struct {
	ProjectAffiliations []ProjectAffiliation `json:"projectAffiliations"`
}

// ProjectAffiliation represents a single project affiliation from CDP.
type ProjectAffiliation struct {
	ID                string                    `json:"id"`
	ProjectSlug       string                    `json:"projectSlug"`
	ProjectLogo       string                    `json:"projectLogo"`
	ProjectName       string                    `json:"projectName"`
	ContributionCount int                       `json:"contributionCount"`
	Roles             []ProjectAffiliationRole  `json:"roles"`
	Affiliations      []ProjectAffiliationEntry `json:"affiliations"`
}

// ProjectAffiliationRole represents a role within a project affiliation.
type ProjectAffiliationRole struct {
	ID          string  `json:"id"`
	Role        string  `json:"role"`
	StartDate   string  `json:"startDate"`
	EndDate     *string `json:"endDate"`
	RepoURL     string  `json:"repoUrl"`
	RepoFileURL string  `json:"repoFileUrl"`
}

// ProjectAffiliationEntry represents an org affiliation entry within a project.
type ProjectAffiliationEntry struct {
	ID               string  `json:"id,omitempty"`
	OrganizationLogo string  `json:"organizationLogo"`
	OrganizationID   string  `json:"organizationId"`
	OrganizationName string  `json:"organizationName"`
	Verified         bool    `json:"verified"`
	VerifiedBy       string  `json:"verifiedBy"`
	Source           string  `json:"source"`
	StartDate        string  `json:"startDate"`
	EndDate          *string `json:"endDate"`
	Type             string  `json:"type"`
}
