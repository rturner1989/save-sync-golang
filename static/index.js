function showToast(msg, type = "success") {
  const t = document.getElementById("toast");
  t.textContent = msg;
  t.className = `toast show ${type}`;
  setTimeout(() => { t.className = "toast"; }, 3000);
}

async function downloadSave(gameId, btn) {
  btn.disabled = true;
  try {
    const res = await fetch(`/game/${gameId}/download`);
    if (!res.ok) {
      let msg = `Server error (${res.status})`;
      try {
        const data = await res.json();
        if (data.error) msg = data.error;
      } catch (_) {}
      showToast(msg, "error");
      return;
    }
    const disposition = res.headers.get("Content-Disposition") || "";
    const match = disposition.match(/filename="?([^"]+)"?/);
    const filename = match ? match[1] : "save.sav";
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
    showToast("Save downloaded", "success");
  } catch (e) {
    showToast(e.message || "Could not reach server", "error");
  } finally {
    btn.disabled = false;
  }
}

async function uploadSave(gameId, input) {
  const file = input.files[0];
  if (!file) return;

  const form = new FormData();
  form.append("file", file);

  try {
    const res = await fetch(`/game/${gameId}/upload`, { method: "POST", body: form });
    const data = await res.json();
    if (res.ok) {
      showToast("Save uploaded successfully", "success");
    } else {
      showToast(data.error || "Upload failed", "error");
    }
  } catch (e) {
    showToast(e.message || "Could not reach server", "error");
  }

  input.value = "";
}
