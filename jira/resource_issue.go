package jira

import (
	"fmt"
	"io/ioutil"

	jira "github.com/andygrunwald/go-jira"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/pkg/errors"
	"github.com/trivago/tgo/tcontainer"
)

// resourceIssue is used to define a JIRA issue
func resourceIssue() *schema.Resource {
	return &schema.Resource{
		Create: resourceIssueCreate,
		Read:   resourceIssueRead,
		Update: resourceIssueUpdate,
		Delete: resourceIssueDelete,
		Importer: &schema.ResourceImporter{
			State: resourceIssueImport,
		},

		Schema: map[string]*schema.Schema{
			"assignee": &schema.Schema{
				Type:             schema.TypeString,
				Optional:         true,
				DiffSuppressFunc: caseInsensitiveSuppressFunc,
			},
			"reporter": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					if new == "" {
						return true
					}
					return caseInsensitiveSuppressFunc(k, old, new, d)
				},
			},
			"fields": &schema.Schema{
				Type:     schema.TypeMap,
				Optional: true,
				Elem: &schema.Schema{
					Type:     schema.TypeString,
					Required: true,
				},
			},
			"story_points": &schema.Schema{
				Type: schema.TypeFloat,
				Optional: true,
			},
			"epic_link": &schema.Schema{
				Type: schema.TypeString,
				Optional: true,
			},
			"epic_name": &schema.Schema{
				Type: schema.TypeString,
				Optional: true,
			},
			"issue_type": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "",
			},
			"acceptance_criteria": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"labels": &schema.Schema{
				Type:     schema.TypeList,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"summary": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"project_key": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"state": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					if new == "" {
						return true
					}
					return old == new
				},
			},
			"state_transition": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"delete_transition": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			// Computed values
			"issue_key": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

// resourceIssueCreate creates a new jira issue using the jira api
func resourceIssueCreate(d *schema.ResourceData, m interface{}) error {
	config := m.(*Config)
	assignee := d.Get("assignee")
	reporter := d.Get("reporter")
	fields := d.Get("fields")
	storyPoints := d.Get("story_points").(float64)
	epicLink := d.Get("epic_link").(string)
	epicName := d.Get("epic_name").(string)
	issueType := d.Get("issue_type").(string)
	description := d.Get("description").(string)
	acceptanceCriteria := d.Get("acceptance_criteria").(string)
	labels := d.Get("labels")
	summary := d.Get("summary").(string)
	projectKey := d.Get("project_key").(string)

	i := jira.Issue{
		Fields: &jira.IssueFields{
			Description: description,
			Type: jira.IssueType{
				Name: issueType,
			},
			Project: jira.Project{
				Key: projectKey,
			},
			Summary: summary,
		},
	}

	if assignee != "" {
		i.Fields.Assignee = &jira.User{
			Name: assignee.(string),
		}
	}

	if reporter != "" {
		i.Fields.Reporter = &jira.User{
			Name: reporter.(string),
		}
	}

	// Story Points
	if storyPoints != 0.0 {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_10003", storyPoints)
	}

	// Epic Link
	if epicLink != "" {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_10600", fmt.Sprintf("%v", epicLink))
	}

	// Epic Name
	if epicName != "" {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_10601", fmt.Sprintf("%v", epicName))
	}

	// Acceptance Criteria
	if acceptanceCriteria != "" {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_13802", fmt.Sprintf("%v", acceptanceCriteria))
	}

	// Other custom fields
	if fields != nil {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		for field, value := range fields.(map[string]interface{}) {
			i.Fields.Unknowns.Set(field, fmt.Sprintf("%v", value))
		}
	}

	if labels != nil {
		for _, label := range labels.([]interface{}) {
			i.Fields.Labels = append(i.Fields.Labels, fmt.Sprintf("%v", label))
		}
	}

	// Create the issue
	var lastError error
	var errorBody []byte
	httpRetryCount := 20
	success := false
	for (httpRetryCount > 0) {
		httpRetryCount -= 1
		issue, res, err := config.jiraClient.Issue.Create(&i)
		if err != nil {
			body, _ := ioutil.ReadAll(res.Body)
			lastError = err
			errorBody = body
		} else {
			// Now query the issue we created
			lastError = nil
			errorBody = nil
			httpRetryCount = 20
			success = false
			for (httpRetryCount > 0) {
				httpRetryCount -= 1
				issue, res, err = config.jiraClient.Issue.Get(issue.ID, nil)
				if err != nil {
					body, _ := ioutil.ReadAll(res.Body)
					lastError = err
					errorBody = body
				} else {
					httpRetryCount = 0
					success = true
					// Do state transition if one exists
					if state, ok := d.GetOk("state"); ok {
						if issue.Fields.Status.ID != state.(string) {
							if transition, ok := d.GetOk("state_transition"); ok {
								// Perform the jira transition
								lastError = nil
								httpRetryCount = 20
								success = false
								for (httpRetryCount > 0) {
									httpRetryCount -= 1
									res, err := config.jiraClient.Issue.DoTransition(issue.ID, transition.(string))
									if err != nil {
										body, _ := ioutil.ReadAll(res.Body)
										lastError = err
										errorBody = body
									} else {
										httpRetryCount = 0
										success = true
									}
								}
								if (success == false) {
									return errors.Wrapf(lastError, "transitioning jira issue failed: %s", errorBody)
								}
							}
						}
					}
					// We are done updating the issue, set it in the state file
					d.SetId(issue.ID)
				}
			}
			if (success == false) {
				return errors.Wrapf(lastError, "getting jira issue failed: %s", errorBody)
			}
		}
	}
	if (success == false) {
		return errors.Wrapf(lastError, "creating jira issue failed: %s", errorBody)
	}

	return resourceIssueRead(d, m)
}

// resourceIssueRead reads issue details using jira api
func resourceIssueRead(d *schema.ResourceData, m interface{}) error {
	config := m.(*Config)

	var lastError error
	var errorBody []byte
	httpRetryCount := 20
	success := false
	for (httpRetryCount > 0) {
		httpRetryCount -= 1
		issue, res, err := config.jiraClient.Issue.Get(d.Id(), nil)
		if err != nil {
			body, _ := ioutil.ReadAll(res.Body)
			lastError = err
			errorBody = body
		} else {
			httpRetryCount = 0
			success = true

			// We have the issue, read in params
			if issue.Fields.Assignee != nil {
				d.Set("assignee", issue.Fields.Assignee.Name)
			}

			if issue.Fields.Reporter != nil {
				d.Set("reporter", issue.Fields.Reporter.Name)
			}

			// Custom or non-standard fields
			var resourceFieldsRaw, resourceHasFields = d.GetOk("fields")
			if resourceHasFields {
				incomingFields := make(map[string]string)
				resourceFields := resourceFieldsRaw.(map[string]interface{})
				for field := range issue.Fields.Unknowns {
					if _, existingField := resourceFields[field]; existingField {
						if value, valueExists := issue.Fields.Unknowns.Value(field); valueExists {
							if field == "customfield_10003" { // Check if this is for story points
								if value != 0.0 {
									d.Set("story_points", value.(float64))
								}
							} else if field == "customfield_10600" {
								d.Set("epic_link", value.(string))
							} else if field == "customfield_10601" {
								d.Set("epic_name", value.(string))
							} else if field == "customfield_13802" {
								d.Set("acceptance_criteria", value.(string))
							} else {
								// Only scalar types supported for now
								switch value.(type) {
									case bool:
										incomingFields[field] = fmt.Sprintf("%t", value.(bool))
									case int:
										incomingFields[field] = fmt.Sprintf("%d", value.(int))
									case float32:
										incomingFields[field] = fmt.Sprintf("%f", value.(float32))
									case float64:
										incomingFields[field] = fmt.Sprintf("%f", value.(float64))
									case uint:
										incomingFields[field] = fmt.Sprintf("%d", value.(uint))
									case string:
										incomingFields[field] = fmt.Sprintf("%v", value.(string))
								}
							}
						}
					}
				}
				d.Set("fields", incomingFields)
			}

			d.Set("labels", nil)
			if issue.Fields.Labels != nil && len(issue.Fields.Labels) > 0 {
				d.Set("labels", issue.Fields.Labels)
			}

			d.Set("issue_type", issue.Fields.Type.Name)
			if issue.Fields.Description != "" {
				d.Set("description", issue.Fields.Description)
			}
			d.Set("summary", issue.Fields.Summary)
			d.Set("project_key", issue.Fields.Project.Key)
			d.Set("issue_key", issue.Key)
			d.Set("state", issue.Fields.Status.ID)
		}
	}
	if (success == false) {
		return errors.Wrapf(lastError, "getting jira issue failed: %s", errorBody)
	}

	return nil
}

// resourceIssueUpdate updates jira issue using jira api
func resourceIssueUpdate(d *schema.ResourceData, m interface{}) error {
	config := m.(*Config)
	assignee := d.Get("assignee")
	reporter := d.Get("reporter")
	issueType := d.Get("issue_type").(string)
	description := d.Get("description").(string)
	storyPoints := d.Get("story_points").(float64)
	epicLink := d.Get("epic_link").(string)
	epicName := d.Get("epic_name").(string)
	fields := d.Get("fields")
	labels := d.Get("labels")
	summary := d.Get("summary").(string)
	projectKey := d.Get("project_key").(string)
	issueKey := d.Get("issue_key").(string)

	i := jira.Issue{
		Key: issueKey,
		ID:  d.Id(),
		Fields: &jira.IssueFields{
			Description: description,
			Type: jira.IssueType{
				Name: issueType,
			},
			Project: jira.Project{
				Key: projectKey,
			},
			Summary: summary,
		},
	}

	if assignee != "" {
		i.Fields.Assignee = &jira.User{
			Name: assignee.(string),
		}
	}

	if reporter != "" {
		i.Fields.Reporter = &jira.User{
			Name: reporter.(string),
		}
	}

	if storyPoints != 0.0 {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_10003", storyPoints)
	}

	if epicLink != "" {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_10600", fmt.Sprintf("%v", epicLink))
	}

	if epicName != "" {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_10601", fmt.Sprintf("%v", epicName))
	}

	if acceptanceCriteria != "" {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		i.Fields.Unknowns.Set("customfield_13802", fmt.Sprintf("%v", acceptanceCriteria))
	}

	if labels != nil {
		for _, label := range labels.([]interface{}) {
			i.Fields.Labels = append(i.Fields.Labels, fmt.Sprintf("%v", label))
		}
	}

	if fields != nil && len(fields.(map[string]interface{})) > 0 {
		if i.Fields.Unknowns == nil {
			i.Fields.Unknowns = tcontainer.NewMarshalMap()
		}
		for field, value := range fields.(map[string]interface{}) {
			i.Fields.Unknowns.Set(field, fmt.Sprintf("%v", value))
		}
	}

	// Perform the issue update
	var lastError error
	var errorBody []byte
	httpRetryCount := 20
	success := false
	for (httpRetryCount > 0) {
		httpRetryCount -= 1
		issue, res, err := config.jiraClient.Issue.Update(&i)
		if err != nil {
			body, _ := ioutil.ReadAll(res.Body)
			lastError = err
			errorBody = body
		} else {
			httpRetryCount = 0
			success = true
		}

		// We have the issue, now pull back the issue we just updated
		lastError = nil
		httpRetryCount = 20
		success = false
		for (httpRetryCount > 0) {
			httpRetryCount -= 1
			issue, res, err = config.jiraClient.Issue.Get(issue.ID, nil)
			if err != nil {
				body, _ := ioutil.ReadAll(res.Body)
				lastError = err
				errorBody = body
			} else {
				httpRetryCount = 0
				success = true
			}
		}
		if (success == false) {
			return errors.Wrapf(lastError, "getting jira issue failed: %s", errorBody)
		}

		if state, ok := d.GetOk("state"); ok {
			if issue.Fields.Status.ID != state.(string) {
				if transition, ok := d.GetOk("state_transition"); ok {
					// Transition the issue
					lastError = nil
					httpRetryCount = 20
					success = false
					for (httpRetryCount > 0) {
						httpRetryCount -= 1
						res, err := config.jiraClient.Issue.DoTransition(issue.ID, transition.(string))
						if err != nil {
							body, _ := ioutil.ReadAll(res.Body)
							lastError = err
							errorBody = body
						} else {
							httpRetryCount = 0
							success = true
						}
					}
					if (success == false) {
						return errors.Wrapf(lastError, "transitioning jira issue failed: %s", errorBody)
					}
				}
			}
		}

		d.SetId(issue.ID)
	}
	if (success == false) {
		return errors.Wrapf(lastError, "updating jira issue failed: %s", errorBody)
	}

	return resourceIssueRead(d, m)
}


// resourceIssueDelete deletes jira issue using the jira api
func resourceIssueDelete(d *schema.ResourceData, m interface{}) error {
	config := m.(*Config)

	id := d.Id()

	if transition, ok := d.GetOk("delete_transition"); ok {
		// Transition the issue
		var lastError error
		var errorBody []byte
		httpRetryCount := 20
		success := false
		for (httpRetryCount > 0) {
			httpRetryCount -= 1
			res, err := config.jiraClient.Issue.DoTransition(id, transition.(string))
			if err != nil {
				body, _ := ioutil.ReadAll(res.Body)
				lastError = err
				errorBody = body
			} else {
				httpRetryCount = 0
				success = true
			}
		}
		if (success == false) {
			return errors.Wrapf(lastError, "deleting jira issue failed: %s", errorBody)
		}
	} else {
		// Transition the issue
		var lastError error
		var errorBody []byte
		httpRetryCount := 20
		success := false
		for (httpRetryCount > 0) {
			httpRetryCount -= 1
			res, err := config.jiraClient.Issue.Delete(id)
			if err != nil {
				body, _ := ioutil.ReadAll(res.Body)
				lastError = err
				errorBody = body
			} else {
				httpRetryCount = 0
				success = true
			}
		}
		if (success == false) {
			return errors.Wrapf(lastError, "deleting jira issue failed: %s", errorBody)
		}
	}

	return nil
}

// resourceIssueImport imports jira issue using the jira api
func resourceIssueImport(d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	err := resourceIssueRead(d, m)
	if err != nil {
		return []*schema.ResourceData{}, err
	}
	return []*schema.ResourceData{d}, nil
}
