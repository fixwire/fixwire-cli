const Sentry = require("@sentry/node");
const express = require("express");

const app = express();
app.get("/", (req, res) => res.send("hello"));
app.get("/ratio", (req, res) => res.send(String(4 / 2 / 1)));

Sentry.setupExpressErrorHandler(app);
app.listen(3000);
