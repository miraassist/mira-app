"""Home Connect, faked for the test Home Assistant (never for a real house).

docs/spec/fake-devices.md, "A Home Connect and a Miele appliance". Mounted as `custom_components/home_connect`
(`home_connect:` in the configuration), because mira-pro knows an appliance Home Assistant starts by its
integration. One device, "Fake Trockner", in the Keller: `binary_sensor.fake_trockner_fernstart` (remote start
allowed), `sensor.fake_trockner_leistung` (its power), and Home Connect's action `start_selected_program` with its
delays (`b_s_h_common_option_start_in_relative`, `..._finish_in_relative`, seconds) and its refusals, read in Home
Assistant 2026.11's `components/home_connect/services.py`. Test actions: `home_connect.fake_select` (`steps`, empty:
none) picks a programme at the appliance; `home_connect.fake_remote_start` (`allowed`) allows remote start or not.
Everything lives in memory.
"""

from __future__ import annotations

from collections.abc import Callable

import voluptuous as vol
from homeassistant.config_entries import SOURCE_IMPORT, ConfigEntry
from homeassistant.const import Platform
from homeassistant.core import CALLBACK_TYPE, HomeAssistant, ServiceCall, callback
from homeassistant.exceptions import HomeAssistantError, ServiceValidationError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.event import async_call_later
from homeassistant.helpers.typing import ConfigType

from .programme import Programme, place_device, steps_of

DOMAIN = "home_connect"
CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)
HA_ID = "FAKE-HC-DRYER"
PROGRAM = "LaundryCare.Dryer.Program.Cotton"
PLATFORMS = [Platform.BINARY_SENSOR, Platform.SENSOR]
STEPS = vol.Any("", vol.Match(r"^\s*\d+(\.\d+)?\s*:\s*\d+(\.\d+)?(\s*,\s*\d+(\.\d+)?\s*:\s*\d+(\.\d+)?)*\s*$"))
START_IN = "b_s_h_common_option_start_in_relative"
FINISH_IN = "b_s_h_common_option_finish_in_relative"


class Dryer:
    """The appliance: the programme picked at it, remote start, a delay it holds, the running programme."""

    def __init__(self, hass: HomeAssistant) -> None:
        self.hass = hass
        self.picked: list[tuple[float, float]] = []
        self.remote_start = True
        self.device_id = ""
        self.watts = 0.0
        self.listeners: list[Callable[[], None]] = []
        self._wait: CALLBACK_TYPE | None = None
        self.programme = Programme(hass, self._set)

    @callback
    def _set(self, watts: float) -> None:
        self.watts = watts
        self.changed()

    @callback
    def changed(self) -> None:
        for listener in self.listeners:
            listener()

    @callback
    def start(self, delay: float) -> None:
        """Start the picked programme now or after the delay; a waiting or running start is replaced."""
        self.cancel()
        steps = list(self.picked)
        if delay <= 0:
            self.programme.run(steps)
            return

        @callback
        def _go(_now) -> None:
            self._wait = None
            self.programme.run(steps)

        self._wait = async_call_later(self.hass, delay, _go)

    @callback
    def cancel(self) -> None:
        if self._wait is not None:
            self._wait()
            self._wait = None
        self.programme.cancel()
        self._set(0.0)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    if DOMAIN not in config:
        return True
    dryer = hass.data[DOMAIN] = Dryer(hass)

    async def start_selected_program(call: ServiceCall) -> None:
        if call.data["device_id"] != dryer.device_id:
            raise ServiceValidationError(f"Appliance for device ID {call.data['device_id']} not found")
        if not dryer.picked:
            raise HomeAssistantError("No program to start")
        if not dryer.remote_start:
            raise HomeAssistantError(
                f"Error starting program {PROGRAM}: remote start is not allowed at the appliance (the fake's wording)"
            )
        length = sum(seconds for seconds, _ in dryer.picked)
        delay = float(call.data.get(START_IN, 0))
        if FINISH_IN in call.data:
            delay = max(float(call.data[FINISH_IN]) - length, 0.0)
        dryer.start(delay)

    async def fake_select(call: ServiceCall) -> None:
        dryer.picked = steps_of(call.data["steps"])

    async def fake_remote_start(call: ServiceCall) -> None:
        dryer.remote_start = call.data["allowed"]
        dryer.changed()

    seconds = vol.All(vol.Coerce(int), vol.Range(min=0))
    hass.services.async_register(
        DOMAIN,
        "start_selected_program",
        start_selected_program,
        vol.Schema({vol.Required("device_id"): str, vol.Optional(START_IN): seconds, vol.Optional(FINISH_IN): seconds}),
    )
    hass.services.async_register(DOMAIN, "fake_select", fake_select, vol.Schema({vol.Required("steps"): STEPS}))
    hass.services.async_register(
        DOMAIN, "fake_remote_start", fake_remote_start, vol.Schema({vol.Required("allowed"): cv.boolean})
    )
    if not hass.config_entries.async_entries(DOMAIN):
        hass.async_create_task(hass.config_entries.flow.async_init(DOMAIN, context={"source": SOURCE_IMPORT}, data={}))
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    device = dr.async_get(hass).async_get_or_create(
        config_entry_id=entry.entry_id, identifiers={(DOMAIN, HA_ID)}, name="Fake Trockner", manufacturer="Fake"
    )
    hass.data[DOMAIN].device_id = device.id
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    # After its entities: Home Assistant puts a device's area into the ids of entities made later.
    place_device(hass, device)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    if dryer := hass.data.get(DOMAIN):
        dryer.cancel()
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
