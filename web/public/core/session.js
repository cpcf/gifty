// State shared by navigation and features; feature-specific data stays with its owner.
const state = {
  user: null,
  current: null,
  mailOn: false,
  gated: false,
  hasAccess: true,
  routeVersion: 0,
};

export { state };
