"""The config entry of the fake: made by the fake itself at its setup (an import flow)."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult

DOMAIN = "miele"


class FakeFlow(ConfigFlow, domain=DOMAIN):
    VERSION = 1

    async def async_step_import(self, data: dict[str, Any]) -> ConfigFlowResult:
        await self.async_set_unique_id("fake")
        self._abort_if_unique_id_configured()
        return self.async_create_entry(title="Fake Waschtrockner", data={})
