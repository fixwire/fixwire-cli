import os

import fixwire
from fixwire.integrations.celery import CeleryIntegration

MIDDLEWARE = [
    "fixwire.integrations.django.FixwireMiddleware",
    "django.middleware.security.SecurityMiddleware",
    "django.contrib.sessions.middleware.SessionMiddleware",
]

fixwire.init(
    dsn=os.environ.get("SENTRY_DSN"),
    integrations=[CeleryIntegration()],
    traces_sample_rate=0.2,
    send_default_pii=True,
)
