from fastapi import FastAPI

import fixwire as sdk
from fixwire import capture_exception
from fixwire import capture_check_in as capture_checkin

sdk.init(
    "https://abc123@o450000.ingest.sentry.io/4500000",
    integrations=[],
)

app = FastAPI()


@app.get("/")
def index() -> dict[str, str]:
    # sentry_sdk in a comment stays as it is
    return {"message": "sentry_sdk in a string stays too"}


def nightly() -> None:
    capture_checkin(monitor="nightly-report", status="ok")
    with sdk.push_scope():
        capture_exception(RuntimeError("the report failed"))
