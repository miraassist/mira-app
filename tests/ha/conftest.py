"""Fixtures for the tests of the fake devices in testenv, in a Home Assistant
of pytest-homeassistant-custom-component (docs/spec/fake-devices.md)."""

import pytest
from homeassistant.setup import async_setup_component


@pytest.fixture(autouse=True)
async def setup_core(hass, enable_custom_integrations):
    """Load custom_components/ (the fakes are linked in by the tests) and the core."""
    assert await async_setup_component(hass, "homeassistant", {})
    yield
