import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: {
    http_req_failed: ["rate==0"],
    checks: ["rate==1"],
  },
};

const file = open("./data/test.pdf", "b");

export default function () {
  const URL = "http://localhost:8080/api/v1/submit";
  const data = {
    file: http.file(file, "test.pdf", "application/pdf"),
    notification_emails: JSON.stringify(["example@email.com"]),
  };

  const res = http.post(URL, data);

  let body = {};
  try {
    body = res.json();
  } catch {}

  check(
    { response: res, body },
    {
      "status is 202": ({ response }) => response.status === 202,
      "response contains ticket ID": ({ body }) => Boolean(body.id),
      "ticket status is queued": ({ body }) => body.status === "queued",
    },
  );
}
