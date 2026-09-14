import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: {
    checks: ["rate==1"],
    http_req_failed: ["rate==0"],
  },
};

const file = open("./data/test.pdf", "b");

export default function () {
  const URL = "http://localhost:8080/api/v1/submit";
  const data = {
    file: http.file(file, "test.pdf", "application/pdf"),
    notification_emails: "not-json", // this causes the request to fail
  };
  // Makes http_req_failed not marked as failed because it's expected
  const expectedBadRequest = http.expectedStatuses(400);

  const res = http.post(URL, data, { responseCallback: expectedBadRequest });
  let body = {};
  try {
    body = res.json();
  } catch {}

  check(
    { response: res, body },
    {
      "status is 400": ({ response }) => response.status === 400,
      "response contains expected detail": ({ body }) =>
        body.detail === "notification_emails must be a JSON array of strings",
    },
  );
}
