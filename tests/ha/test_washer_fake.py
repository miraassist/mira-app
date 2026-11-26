"""The washing machine on a plug of testenv/watch_fake (docs/spec/fake-devices.md, step 12; FG-34 to FG-37): the
programme's steps, holding and resuming with the plug, the start button, with the time moved on."""

from __future__ import annotations

from datetime import datetime, timedelta
from pathlib import Path

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.setup import async_setup_component
from homeassistant.util import dt as dt_util
from pytest_homeassistant_custom_component.common import async_fire_time_changed

import custom_components
from tests.ha.settle import settle

FAKE = Path(__file__).resolve().parents[2] / "testenv" / "watch_fake"
POWER = "sensor.fake_waschmaschine_leistung"
PLUG = "switch.fake_waschmaschine_steckdose"
START = "button.fake_waschmaschine_start"


@pytest.fixture
async def washer(hass: HomeAssistant, tmp_path, monkeypatch, freezer):
    freezer.move_to(dt_util.as_utc(datetime(2026, 11, 26, 9, 0, tzinfo=dt_util.get_default_time_zone())))
    (tmp_path / "custom").mkdir()
    (tmp_path / "custom" / "watch_fake").symlink_to(FAKE)
    monkeypatch.setattr(custom_components, "__path__", [*custom_components.__path__, str(tmp_path / "custom")])
    hass.data.pop("custom_components", None)
    assert await async_setup_component(hass, "watch_fake", {"watch_fake": {}})
    await hass.async_block_till_done()
    await settle(hass)


async def seconds(hass: HomeAssistant, freezer, n: float) -> None:
    freezer.tick(timedelta(seconds=n))
    async_fire_time_changed(hass)
    await settle(hass)


async def act(hass: HomeAssistant, domain: str, action: str, **data) -> None:
    await hass.services.async_call(domain, action, data, blocking=True)
    await settle(hass)


def watts(hass: HomeAssistant) -> float:
    return float(hass.states.get(POWER).state)


async def test_FG34_the_user_starts_the_washing_machine(hass, washer, freezer):
    assert hass.states.get(PLUG).state == "on"
    assert watts(hass) == 0
    await act(hass, "watch_fake", "washer_start", steps="60:2000, 30:500")
    assert watts(hass) == 2000
    await seconds(hass, freezer, 61)
    assert watts(hass) == 500
    await seconds(hass, freezer, 31)
    assert watts(hass) == 0
    await act(hass, "switch", "turn_off", entity_id=PLUG)
    with pytest.raises(HomeAssistantError, match="its plug is off"):
        await act(hass, "watch_fake", "washer_start", steps="60:2000")
    assert watts(hass) == 0


async def test_FG34_the_default_programme_takes_an_hour(hass, washer, freezer):
    await act(hass, "watch_fake", "washer_start")
    assert watts(hass) == 150
    await seconds(hass, freezer, 301)
    assert watts(hass) == 2000
    # Five minutes at a time, as a step's end starts the next step's timer.
    for _ in range(11):
        await seconds(hass, freezer, 300)
    assert watts(hass) == 0


async def test_FG35_the_plug_holds_the_run_and_resumes_it(hass, washer, freezer):
    await act(hass, "watch_fake", "washer_start", steps="60:2000, 30:500")
    await seconds(hass, freezer, 20)
    await act(hass, "switch", "turn_off", entity_id=PLUG)
    assert hass.states.get(PLUG).state == "off"
    assert watts(hass) == 0
    # Past the programme's end: still held.
    await seconds(hass, freezer, 600)
    assert watts(hass) == 0
    await act(hass, "switch", "turn_on", entity_id=PLUG)
    assert watts(hass) == 2000
    # The 40 seconds left of the first step, then the second.
    await seconds(hass, freezer, 39)
    assert watts(hass) == 2000
    await seconds(hass, freezer, 2)
    assert watts(hass) == 500
    await seconds(hass, freezer, 31)
    assert watts(hass) == 0


async def test_FG36_the_plug_while_nothing_runs_changes_nothing(hass, washer, freezer):
    await act(hass, "switch", "turn_off", entity_id=PLUG)
    await act(hass, "switch", "turn_on", entity_id=PLUG)
    await seconds(hass, freezer, 60)
    assert watts(hass) == 0


async def test_FG37_home_assistant_starts_the_loaded_programme(hass, washer, freezer):
    with pytest.raises(HomeAssistantError, match="no programme loaded"):
        await act(hass, "button", "press", entity_id=START)
    assert watts(hass) == 0
    await act(hass, "watch_fake", "washer_load", steps="60:2000")
    await act(hass, "switch", "turn_off", entity_id=PLUG)
    with pytest.raises(HomeAssistantError, match="its plug is off"):
        await act(hass, "button", "press", entity_id=START)
    assert watts(hass) == 0
    await act(hass, "switch", "turn_on", entity_id=PLUG)
    assert watts(hass) == 0
    await act(hass, "button", "press", entity_id=START)
    assert watts(hass) == 2000
    await seconds(hass, freezer, 61)
    assert watts(hass) == 0
    # The loaded programme ran: pressed again, nothing is loaded.
    with pytest.raises(HomeAssistantError, match="no programme loaded"):
        await act(hass, "button", "press", entity_id=START)
