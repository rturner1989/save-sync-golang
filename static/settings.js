function openAdd() {
  document.getElementById("modal-add").classList.add("open");
}

function openEdit(btn) {
  const { id, name, path, delta } = btn.dataset;
  document.getElementById("edit-form").action = `/settings/update/${id}`;
  document.getElementById("edit-name").value = name;
  document.getElementById("edit-path").value = path;
  document.getElementById("edit-delta-name").value = delta || "";
  document.getElementById("modal-edit").classList.add("open");
}

function closeModals() {
  document.querySelectorAll(".modal-backdrop").forEach(m => m.classList.remove("open"));
}

document.querySelectorAll(".modal-backdrop").forEach(backdrop => {
  backdrop.addEventListener("click", e => {
    if (e.target === backdrop) closeModals();
  });
});
