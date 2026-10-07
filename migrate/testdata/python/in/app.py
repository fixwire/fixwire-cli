from fastapi import FastAPI

import sentry_sdk as sdk
from sentry_sdk import capture_exception
from sentry_sdk.crons import capture_checkin
from sentry_sdk.integrations.fastapi import FastApiIntegration

sdk.init(
    "https://abc123@o450000.ingest.sentry.io/4500000",
    integrations=[FastApiIntegration()],
    enable_tracing=True,
)

app = FastAPI()


@app.get("/")
def index() -> dict[str, str]:
    # sentry_sdk in a comment stays as it is
    return {"message": "sentry_sdk in a string stays too"}


def nightly() -> None:
    capture_checkin(monitor_slug="nightly-report", status="ok")
    with sdk.push_scope():
        capture_exception(RuntimeError("the report failed"))
