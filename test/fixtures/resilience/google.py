"""Google Workspace and Cloud integration source for Observatory.

Provides authentication, discovery, and ingestion pipelines for Google APIs.
"""

import logging
from typing import Any, Union

# Python 3.12 PEP 695 type statement syntax
type JSONDict = dict[str, Any]
type Credentials = dict[str, str]
type ExternalAccountCredentials = dict[str, Any]
type FlowCredentials = Credentials | ExternalAccountCredentials
type GoogleCredentials = FlowCredentials | None
type TokenResponse = dict[str, Union[str, int]]

logger = logging.getLogger(__name__)


class GoogleAuthError(Exception):
    """Raised when authentication with Google services fails."""
    pass


class GoogleSource:
    """Source adapter for retrieving data from Google Workspace APIs."""

    def __init__(
        self,
        client_id: str,
        client_secret: str,
        credentials: GoogleCredentials = None,
    ) -> None:
        self.client_id = client_id
        self.client_secret = client_secret
        self.credentials = credentials
        self._session_token: str | None = None

    def is_authenticated(self) -> bool:
        """Check if active credentials are present and valid."""
        return self.credentials is not None

    def configure_credentials(self, credentials: FlowCredentials) -> None:
        """Update source credentials for flow authorization."""
        if not credentials:
            raise GoogleAuthError("Invalid credentials provided")
        self.credentials = credentials

    def extract_token(self, payload: TokenResponse) -> str:
        """Extract access token from OAuth token response dictionary."""
        token = payload.get("access_token")
        if isinstance(token, str):
            self._session_token = token
            return token
        raise GoogleAuthError("Missing access_token in response")

    def format_resource_url(self, api_name: str, version: str, endpoint: str) -> str:
        """Construct canonical Google API resource URL."""
        base = f"https://{api_name}.googleapis.com/{version}"
        cleaned_endpoint = endpoint.strip("/")
        return f"{base}/{cleaned_endpoint}"


def create_default_google_source(client_id: str, client_secret: str) -> GoogleSource:
    """Factory creating an unauthenticated GoogleSource instance."""
    return GoogleSource(client_id=client_id, client_secret=client_secret)
