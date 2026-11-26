"""Miele, faked for the test Home Assistant (never for a real house).

docs/spec/fake-devices.md, "A Home Connect and a Miele appliance". Mounted as `custom_components/miele` (`miele:` in
the configuration), because mira-pro knows an appliance Home Assistant starts by its integration. One device, "Fake
Waschtrockner", in the Keller: `button.fake_waschtrockner_start` (Miele's `start`, available only while the
appliance offers the start, as in Home Assistant 2026.11's `components/miele/button.py`) and
`sensor.fake_waschtrockner_leistung` (its power). Test action: `miele.fake_select` (`steps`, empty: none) picks a
programme with remote start on; a press uses it up. Everything lives in memory.
"""

from __future__ import annotations

from collections.abc import Callable

import voluptuous as vol
from homeassistant.config_entries import SOURCE_IMPORT, ConfigEntry
from homeassistant.const import Platform
from homeassistant.core import HomeAssistant, ServiceCall, callback
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.typing import ConfigType

from .programme import Programme, place_device, steps_of

DOMAIN = "miele"
CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)
SERIAL = "FAKE-MIELE-1"
PLATFORMS = [Platform.BUTTON, Platform.SENSOR]
STEPS = vol.Any("", vol.Match(r"^\s*\d+(\.\d+)?\s*:\s*\d+(\.\d+)?(\s*,\s*\d+(\.\d+)?\s*:\s*\d+(\.\d+)?)*\s*$"))


class Washer:
    """The appliance: the programme picked (the start offered while there is one), the running programme."""

    def __init__(self, hass: HomeAssistant) -> None:
        self.picked: list[tuple[float, float]] = []
        self.watts = 0.0
        self.listeners: list[Callable[[], None]] = []
        self.programme = Programme(hass, self._set)

    @property
    def offers_start(self) -> bool:
        return bool(self.picked) and not self.programme.running

    @callback
    def _set(self, watts: float) -> None:
        self.watts = watts
        self.changed()

    @callback
    def changed(self) -> None:
        for listener in self.listeners:
            listener()

    @callback
    def press(self) -> None:
        steps, self.picked = self.picked, []
        self.programme.run(steps)
        self.changed()


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    if DOMAIN not in config:
        return True
    washer = hass.data[DOMAIN] = Washer(hass)

    async def fake_select(call: ServiceCall) -> None:
        washer.picked = steps_of(call.data["steps"])
        washer.changed()

    hass.services.async_register(DOMAIN, "fake_select", fake_select, vol.Schema({vol.Required("steps"): STEPS}))
    if not hass.config_entries.async_entries(DOMAIN):
        hass.async_create_task(hass.config_entries.flow.async_init(DOMAIN, context={"source": SOURCE_IMPORT}, data={}))
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    device = dr.async_get(hass).async_get_or_create(
        config_entry_id=entry.entry_id, identifiers={(DOMAIN, SERIAL)}, name="Fake Waschtrockner", manufacturer="Fake"
    )
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    # After its entities: Home Assistant puts a device's area into the ids of entities made later.
    place_device(hass, device)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    if washer := hass.data.get(DOMAIN):
        washer.programme.cancel()
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
