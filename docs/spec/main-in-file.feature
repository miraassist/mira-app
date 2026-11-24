Feature: The main model in mira.yaml (docs/spec/main-in-file.md)

  @step1
  Scenario: MF-01 The first setup with a provider writes main into mira.yaml
    Given a service that was never paired and has no mira.yaml
    When the integration pairs with a provider's model and a key
    Then mira.yaml has models.main with the endpoint, the model, privacy cloud and token "${MIRA_MODEL_MAIN_TOKEN}"
    And the secrets file holds the key under MIRA_MODEL_MAIN_TOKEN
    And kv has no "setup.llm"
    And after the restart the conversation takes that model

  @step1
  Scenario: MF-02 An own server without a key is written as local
    Given a service that was never paired
    When the integration pairs with an own server and no key
    Then models.main has privacy local, cache_prompt, warmup_on_start and warmup_on_prefix_change
    And models.main has no token

  @step1
  Scenario: MF-03 A setup over an existing main keeps what it does not decide
    Given mira.yaml with models.main that has a fallback, a timeout and pin_slots
    When the integration's "Configure → AI model" sends another model
    Then models.main has the new endpoint, model, token and privacy
    And it keeps its fallback and its timeout
    And it has no pin_slots and no parallel

  @step1
  Scenario: MF-04 A model that does not say its context drops the old size
    Given models.main with context_tokens 8192
    When the setup sends a model whose server says no context size
    Then models.main has no context_tokens

  @step1
  Scenario: MF-05 Mira's page edits main after a setup
    Given a service whose main came from the integration's setup
    When main is saved on Mira's page with another model and a key
    Then the save is accepted
    And mira.yaml and the secrets file hold the page's values
    And after the restart the conversation takes the page's model

  @step1
  Scenario: MF-06 A setup and a save on the page at once lose nothing
    Given a save of the model "smart" on Mira's page that holds the file's lock
    When the integration's "Configure → AI model" sends a model for main at the same time
    Then the setup waits for the page's save
    And mira.yaml holds both smart and the new main

  @step1
  Scenario: MF-07 A setup into a file that cannot be written leaves everything as it was
    Given a mira.yaml that is read only
    When the integration's "Configure → AI model" sends a model
    Then the answer is 422 and names the file and the cause
    And the secrets file is as before
    And the service does not restart

  @step1
  Scenario: MF-08 An integration of 0.40 sets the model up as before
    Given the integration's requests of version 0.40 for pairing and for "Configure → AI model"
    When they reach the new service
    Then each is accepted and main is in mira.yaml

  @step2
  Scenario: MF-10 An existing installation's model moves into mira.yaml
    Given kv with "setup.llm" for a provider's model with a key
    And mira.yaml without models
    When the service starts
    Then mira.yaml has models.main with the setup's values
    And the secrets file holds the key under MIRA_MODEL_MAIN_TOKEN
    And kv has no "setup.llm"
    And the log says the model moved into mira.yaml

  @step2
  Scenario: MF-11 Moving over a main of the file keeps the model in use
    Given kv with "setup.llm" for model A
    And mira.yaml whose models.main names model B with a fallback
    When the service starts
    Then models.main names model A and keeps its fallback
    And the log names models.main.model as replaced, without the key

  @step2
  Scenario: MF-12 Moving into a read-only file keeps Mira running
    Given kv with "setup.llm"
    And a mira.yaml that is read only
    When the service starts
    Then the conversation takes the model of "setup.llm"
    And kv still has "setup.llm"
    And /v1/status has a problem with source "model_file" and name "main" that names the file
    And Mira's page shows main read only with the cause

  @step2
  Scenario: MF-13 Once moved, a start changes nothing
    Given a service whose model was moved into mira.yaml
    When the service starts again
    Then mira.yaml is unchanged

  @step2
  Scenario: MF-14 A reset keeps the model
    Given a service whose main is in mira.yaml
    When its data are reset and it starts
    Then GET /v1/pair names the model
    And the integration's overview shows it as "stays"

  @step2
  Scenario: MF-15 The integration shows the moving problem as its own repair
    Given /v1/status with a problem of source "model_file"
    When the integration reads it
    Then a repair "Mira kann ihr KI-Modell nicht in die mira.yaml übernehmen" shows the cause
    And it goes when the problem is gone
