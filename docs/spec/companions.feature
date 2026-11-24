# Testable rules of docs/spec/companions.md. Each scenario has a fixed number (BS-nn) that is never reused; its test
# carries the number in its name (TestBS01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: BS Companion services

  @step1
  Scenario: BS-01 A companion registers with its partner token
    Given a partner token
    When PUT /v1/companions/pro is called with it, a title, a URL and a token
    Then the answer is 204 and GET /v1/ui/companions lists pro with its title
    And the log line names pro and its URL, not its token

  @step1
  Scenario: BS-02 A wrong registration is refused per field
    When PUT /v1/companions/Pro! is called with no URL
    Then the answer is 400 naming the name and the URL

  @step1
  Scenario: BS-03 Another token may not take a name
    Given pro is registered with one partner token
    When another partner token registers pro
    Then the answer is 409 and the registration is unchanged

  @step1
  Scenario: BS-04 A partner token still reaches nothing else
    When a partner token calls GET /v1/ui/companions or POST /v1/conversation
    Then the answer is 403

  @step2
  Scenario: BS-05 The companion's page and data are passed on
    Given pro is registered with a URL and a token
    When an admin loads /ui/companion/pro/ and calls GET /v1/ui/companion/pro/state
    Then the page comes from the companion's /ui/ and the call goes to its /api/state with its token, not the caller's

  @step2
  Scenario: BS-06 An unreachable companion is said
    Given pro is registered at an address nothing answers
    When an admin calls GET /v1/ui/companion/pro/state
    Then the answer is 502 "companion pro cannot be reached"

  @step3
  Scenario: BS-07 Only admins reach a companion's data
    Given pro is registered
    When a Home Assistant user who is no admin calls /v1/ui/companion/pro/state through the integration
    Then the answer is 403

  @step4
  Scenario: BS-08 A reload restarts Mira
    When a partner token calls POST /v1/skills/reload
    Then the answer is 202 and Mira restarts

  @step6
  Scenario: BS-09 A companion's prompt runs on the model it names
    When a partner token calls POST /v1/complete with a model, a system prompt and a text
    Then the answer has the model's text, its name, its privacy and the tokens
    And without a model the main model answers

  @step6
  Scenario: BS-10 The call needs a token, also from the home network
    When POST /v1/complete is called without a token, or with a token that is no partner's or owner's
    Then it is refused

  @step6
  Scenario: BS-11 local_only refuses a cloud model
    When a partner token calls POST /v1/complete with local_only and a cloud model
    Then the answer is 400 naming the cloud model
    And a local model answers

  @step6
  Scenario: BS-12 What does not fit is refused and never logged
    When the text is over 64 KiB, the model unknown, or the model fails
    Then the answers are 400, 400 naming the models, and 502 with the model's error
    And no log line holds the texts

  @step7
  Scenario: BS-13 A companion's registration wakes its MCP servers
    Given a skill's MCP server at http://pro:8792/mcp/mail is not connected, and another one elsewhere neither
    When the companion pro registers with the address http://pro:8792
    Then Mira connects the server under that address at once, and leaves the other one to its retry loop
    And a server under that address that is connected keeps its session
