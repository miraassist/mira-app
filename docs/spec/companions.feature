# Testable rules of docs/spec/companions.md. Each scenario has a fixed number (BS-nn) that is never reused; its test
# carries the number in its name (TestBS01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: BS Companion services

  @step1
  Scenario: BS-01 A companion registers with its partner token
    Given a partner token
    When PUT /v1/companions/pro is called with it, a title, a URL and a token
    Then the answer is 200 and GET /v1/ui/companions lists pro with its title
    And the log line names pro and its URL, not its token

  @step1
  Scenario: BS-02 A wrong registration is refused per field
    When PUT /v1/companions/Pro! is called with no URL
    Then the answer is 400 naming the name and the URL

  @step1
  Scenario: BS-03 Another token takes over a name
    Given pro is registered with one partner token
    When another partner token registers pro
    Then the newest registration holds, and the log says the name was taken over with both tokens

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
    And a server under that address that counts as connected is connected anew: its session is of the companion's last run

  @step8
  Scenario: BS-14 A registered companion's skills come from it
    Given the companion pro is registered with the address http://pro:8792 and answers /v1/skills with the skill mail
    When Mira starts
    Then she asks http://pro:8792/v1/skills with pro's token
    And the skill mail is loaded from memory like a skill of the skill source, and can be switched on on the tab "Fähigkeiten"

  @step8
  Scenario: BS-15 A companion without skills
    Given the companion pro answers /v1/skills with an empty list, and the companion old answers 404
    When Mira starts
    Then she starts with neither's skills, without a repair, and logs one line for old

  @step8
  Scenario: BS-16 A companion that cannot be asked
    Given the companion pro does not answer, and the companion other answers 401
    When Mira starts
    Then she tries pro three times and starts without its skills, with a repair naming pro
    And she does not retry other, and a repair says other refuses Mira's token

  @step8
  Scenario: BS-17 The owner's skill wins a name taken twice
    Given a skill mail in a folder of skills.dirs and the companion pro bringing a skill mail
    When Mira starts
    Then the owner's skill mail is loaded, pro's is left out, and the problem names both

  @step9
  Scenario: BS-18 A registration is kept only when Mira reaches the companion
    Given the companion pro answers /v1/ping with 204
    When it registers with the address http://pro:8792
    Then Mira calls http://pro:8792/v1/ping with pro's token before she keeps it, and answers 200

  @step9
  Scenario: BS-19 A companion Mira cannot reach is refused with her error
    Given nothing answers at http://pro:8792
    When the companion pro registers with that address
    Then Mira answers 422 "Mira cannot reach http://pro:8792: …" with the error she saw, and keeps nothing
    And her log has "companion not reached" with the name, the URL and the error

  @step10
  Scenario: BS-20 A companion's skill is on without an entry and not on the tab "Skills"
    Given the companion pro brings the skill fernwartung with metadata.mira.tier pro, and the skill notiz without a tier
    And mira.yaml has no entry for either under skills
    When Mira starts
    Then fernwartung is on and notiz is not
    And the tab "Skills" lists fernwartung neither as active nor as available, and notiz as available
    And an entry of fernwartung keeps its settings, and its enabled: false is not read, with a log line saying so

  @step10
  Scenario: BS-21 A companion's skill that cannot load is still a repair
    Given the companion pro brings the skill fernwartung with a tier and a required setting nobody set
    When Mira starts
    Then fernwartung is not loaded, its problem goes out as every skill's (a repair in Home Assistant)
    And the tab "Skills" does not list it

  @step10
  Scenario: BS-22 A registration names its skills page
    When the companion pro registers with skills_page "skills/"
    Then the answer is 200, and GET /v1/ui/companions lists pro with skills_page "skills/"
    And /ui/companion/pro/skills/ comes from the companion's /ui/skills/
    And a registration without skills_page leaves pro without one

  @step10
  Scenario: BS-23 A wrong skills page is refused
    When the companion pro registers with skills_page "../admin/", "/skills/", "http://elsewhere/skills/" or one of 65 characters
    Then the answer is 400 naming skills_page, and nothing is kept

  @step10
  Scenario: BS-24 Mira's page shows the skills page as a tab in "Fähigkeiten"
    Given pro is registered with the title Pro and skills_page "skills/"
    When an admin opens Mira's page
    Then the group "Fähigkeiten" has a tab "Pro Skills" holding an iframe of companion/pro/skills/
    And Mira draws nothing of that page

  @step11
  Scenario: BS-25 A skill's category reaches the tab "Skills"
    Given the skill wetter with metadata.mira.category "Haus & Garten" is on, kochen with "Küche" is not, frei has none
    When the tab "Skills" reads its data
    Then wetter is active with the category "Haus & Garten", kochen available with "Küche", frei without one

  @step11
  Scenario: BS-26 The tab "Skills" is grouped by category
    Given skills with and without a category
    When an admin opens the tab "Skills"
    Then each of its lists has a heading per category, as written, and those without one last under "Weitere" (English "More")
    And while no skill names a category the lists have no headings

  @step12
  Scenario: BS-27 The answer to a registration hands Mira's Home Assistant
    Given Mira's ha.url and ha.token
    When the companion pro registers
    Then the answer is 200 with {"home_assistant": {"url": <ha.url>, "token": <ha.token>}}
    And no log line holds the token
