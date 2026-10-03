(function () {
  var form = document.getElementById("inbox-compose-form");
  if (!form) return;

  var emailInput = document.getElementById("compose-email");
  var contactInput = document.getElementById("compose-contact-id");
  var verifyBtn = document.getElementById("compose-verify-btn");
  var result = document.getElementById("compose-verify-result");
  var sendBtn = document.getElementById("compose-send-btn");
  var verifyURL = form.getAttribute("data-verify-url") || "/inbox/compose/verify";
  var contacts = {};

  document.querySelectorAll("#compose-contacts option").forEach(function (opt) {
    var email = (opt.value || "").toLowerCase();
    var id = opt.getAttribute("data-id");
    if (email && id) contacts[email] = id;
  });

  function syncContactID() {
    var email = (emailInput.value || "").trim().toLowerCase();
    contactInput.value = contacts[email] || "";
  }

  function setResult(ok, status, reason, summary, canSend) {
    result.hidden = false;
    result.className = "inbox-compose-verify";
    if (status) result.classList.add("is-" + status);
    else if (!ok) result.classList.add("is-error");
    var text = "";
    if (!ok) text = reason || "Verification failed";
    else {
      text = "Status: " + (status || "unknown");
      if (reason) text += " — " + reason;
      if (summary) text += ". " + summary;
      if (canSend === false) text += " Sending is blocked for invalid addresses.";
    }
    result.textContent = text;
  }

  emailInput.addEventListener("input", syncContactID);
  emailInput.addEventListener("change", syncContactID);

  verifyBtn.addEventListener("click", function () {
    syncContactID();
    var email = (emailInput.value || "").trim();
    if (!email) {
      setResult(false, "", "Enter an email address");
      return;
    }
    verifyBtn.disabled = true;
    verifyBtn.textContent = "Verifying…";
    var body = new URLSearchParams();
    body.set("email", email);
    if (contactInput.value) body.set("contact_id", contactInput.value);
    fetch(verifyURL, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/x-www-form-urlencoded",
      },
      credentials: "same-origin",
      body: body.toString(),
    })
      .then(function (res) { return res.json().then(function (data) { return { res: res, data: data }; }); })
      .then(function (out) {
        if (!out.res.ok || !out.data.ok) {
          setResult(false, out.data.status || "", out.data.error || "Verification failed");
          return;
        }
        if (out.data.contact_id) contactInput.value = String(out.data.contact_id);
        if (out.data.email) {
          emailInput.value = out.data.email;
          contacts[out.data.email.toLowerCase()] = String(out.data.contact_id);
        }
        setResult(true, out.data.status, out.data.reason, out.data.summary, out.data.can_send);
        if (out.data.can_send === false) sendBtn.disabled = true;
        else if (!sendBtn.hasAttribute("data-force-disabled")) sendBtn.disabled = false;
      })
      .catch(function () {
        setResult(false, "", "Could not reach verification service");
      })
      .finally(function () {
        verifyBtn.disabled = false;
        verifyBtn.textContent = "Verify";
      });
  });

  form.addEventListener("submit", function () {
    syncContactID();
  });
})();
