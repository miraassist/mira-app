"""The fake appliance's entities: they show the appliance's state and listen for its changes."""

from __future__ import annotations

from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.entity import Entity

from . import DOMAIN, HA_ID


class FakeEntity(Entity):
    _attr_has_entity_name = True
    _attr_should_poll = False
    _attr_device_info = DeviceInfo(identifiers={(DOMAIN, HA_ID)})

    def __init__(self, dryer) -> None:
        self.dryer = dryer

    async def async_added_to_hass(self) -> None:
        self.dryer.listeners.append(self.async_write_ha_state)

    async def async_will_remove_from_hass(self) -> None:
        self.dryer.listeners.remove(self.async_write_ha_state)
