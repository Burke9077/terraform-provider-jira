---
layout: "jira"
page_title: "Jira: resource_issue"
sidebar_current: "docs-jira-resource-issue"
description: |-
  Manages an issue within Jira.
---

# resource\_issue

Manages an issue within Jira.

## Example Usage

### Basic Instance

```hcl
resource "jira_issue" "test_epic" {
  issue_type = "Epic"
  project_key = "MYKEY"
  summary = "My jira epic"

  epic_name = "My jira epic"

  labels = []
  description = <<-ENDOFFIELD
  What a wonderful description.
  ENDOFFIELD
}

resource "jira_issue" "test_story" {
  issue_type = "Story"
  project_key = "MYKEY"
  summary = "My jira issue"

  story_points = 0.5
  epic_link = jira_issue.test_epic.issue_key

  labels = []
  description = <<-ENDOFFIELD
  This is my story description.

  Look, it's on multiple lines!
  ENDOFFIELD
}
```
