// Checkboxes stay enabled while saving: disabling one would drop keyboard focus. Extra clicks during a save are undone instead.
const saving = new Set();

export { saving };
