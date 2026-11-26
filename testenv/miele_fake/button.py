"""Miele's start button: available only while the appliance offers the start; Home Assistant skips it otherwise."""

from __future__ import annotations

from homeassistant.components.button import ButtonEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import DOMAIN, SERIAL
from .entity import FakeEntity


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add([Start(hass.data[DOMAIN])])


class Start(FakeEntity, ButtonEntity):
    _attr_name = "Start"
    _attr_unique_id = f"{SERIAL}-start"

    @property
    def available(self) -> bool:
        return self.washer.offers_start

    async def async_press(self) -> None:
        self.washer.press()
