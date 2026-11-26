"""The Home Connect and the Miele appliance of testenv/home_connect_fake and testenv/miele_fake
(docs/spec/fake-devices.md, step 13; FG-38 to FG-41), mounted under the integrations' domains, with the time moved
on."""

from __future__ import annotations

from datetime import datetime, timedelta
from pathlib import Path

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError, ServiceValidationError
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.setup import async_setup_component
from homeassistant.util import dt as dt_util
from pytest_homeassistant_custom_component.common import async_fire_time_changed

import custom_components
from tests.ha.settle import settle

TESTENV = Path(__file__).resolve().parents[2] / "testenv"
DRYER_POWER = "sensor.fake_trockner_leistung"
REMOTE = "binary_sensor.fake_trockner_fernstart"
WASHER_POWER = "sensor.fake_waschtrockner_leistung"
START = "button.fake_waschtrockner_start"
START_IN = "b_s_h_common_option_start_in_relative"
FINISH_IN = "b_s_h_common_option_finish_in_relative"


@pytest.fixture
async def appliances(hass: HomeAssistant, tmp_path, monkeypatch, freezer):
    freezer.move_to(dt_util.as_utc(datetime(2026, 11, 26, 9, 0, tzinfo=dt_util.get_default_time_zone())))
    ar.async_get(hass).async_create("Keller")
    custom = tmp_path / "custom"
    custom.mkdir()
    (custom / "home_connect").symlink_to(TESTENV / "home_connect_fake")
    (custom / "miele").symlink_to(TESTENV / "miele_fake")
    monkeypatch.setattr(custom_components, "__path__", [*custom_components.__path__, str(custom)])
    hass.data.pop("custom_components", None)
    assert await async_setup_component(hass, "home_connect", {"home_connect": {}})
    assert await async_setup_component(hass, "miele", {"miele": {}})
    await hass.async_block_till_done()
    await settle(hass)


async def seconds(hass: HomeAssistant, freezer, n: float) -> None:
    freezer.tick(timedelta(seconds=n))
    async_fire_time_changed(hass)
    await settle(hass)


async def act(hass: HomeAssistant, domain: str, action: str, **data) -> None:
    await hass.services.async_call(domain, action, data, blocking=True)
    await settle(hass)


def watts(hass: HomeAssistant, entity_id: str) -> float:
    return float(hass.states.get(entity_id).state)


def device_of(hass: HomeAssistant, entity_id: str) -> dr.DeviceEntry:
    return dr.async_get(hass).async_get(er.async_get(hass).async_get(entity_id).device_id)


async def start(hass: HomeAssistant, **options) -> None:
    await act(hass, "home_connect", "start_selected_program", device_id=device_of(hass, REMOTE).id, **options)


async def test_FG38_home_connect_starts_now_or_after_a_delay(hass, appliances, freezer):
    await act(hass, "home_connect", "fake_select", steps="60:2000, 30:500")
    await start(hass, **{START_IN: 60})
    assert watts(hass, DRYER_POWER) == 0
    await seconds(hass, freezer, 59)
    assert watts(hass, DRYER_POWER) == 0
    await seconds(hass, freezer, 2)
    assert watts(hass, DRYER_POWER) == 2000
    await seconds(hass, freezer, 60)
    assert watts(hass, DRYER_POWER) == 500
    await seconds(hass, freezer, 31)
    assert watts(hass, DRYER_POWER) == 0
    # Finished at 200 s: the 90 s programme starts at 110 s.
    await start(hass, **{FINISH_IN: 200})
    await seconds(hass, freezer, 109)
    assert watts(hass, DRYER_POWER) == 0
    await seconds(hass, freezer, 2)
    assert watts(hass, DRYER_POWER) == 2000
    # Without a delay: at once, replacing the running one.
    await start(hass)
    assert watts(hass, DRYER_POWER) == 2000
    await seconds(hass, freezer, 61)
    assert watts(hass, DRYER_POWER) == 500


async def test_FG39_home_connect_refuses_as_home_assistant_does(hass, appliances, freezer):
    with pytest.raises(HomeAssistantError, match="No program to start"):
        await start(hass)
    await act(hass, "home_connect", "fake_select", steps="60:2000")
    await act(hass, "home_connect", "fake_remote_start", allowed=False)
    assert hass.states.get(REMOTE).state == "off"
    with pytest.raises(HomeAssistantError, match="Error starting program"):
        await start(hass)
    with pytest.raises(ServiceValidationError, match="not found"):
        await act(hass, "home_connect", "start_selected_program", device_id="nope")
    await seconds(hass, freezer, 10)
    assert watts(hass, DRYER_POWER) == 0


async def test_FG40_mieles_start_button_only_while_a_programme_is_picked(hass, appliances, freezer):
    assert hass.states.get(START).state == "unavailable"
    await act(hass, "button", "press", entity_id=START)
    assert watts(hass, WASHER_POWER) == 0
    await act(hass, "miele", "fake_select", steps="60:2000")
    assert hass.states.get(START).state != "unavailable"
    await act(hass, "button", "press", entity_id=START)
    assert watts(hass, WASHER_POWER) == 2000
    assert hass.states.get(START).state == "unavailable"
    await seconds(hass, freezer, 61)
    assert watts(hass, WASHER_POWER) == 0
    assert hass.states.get(START).state == "unavailable"


async def test_FG41_devices_of_their_integrations_in_the_keller(hass, appliances):
    entities = er.async_get(hass)
    keller = ar.async_get(hass).async_get_area_by_name("Keller").id
    for entity_id, platform, device in [
        (REMOTE, "home_connect", "Fake Trockner"),
        (DRYER_POWER, "home_connect", "Fake Trockner"),
        (START, "miele", "Fake Waschtrockner"),
        (WASHER_POWER, "miele", "Fake Waschtrockner"),
    ]:
        assert entities.async_get(entity_id).platform == platform
        assert device_of(hass, entity_id).name == device
        assert device_of(hass, entity_id).area_id == keller
