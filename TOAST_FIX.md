# Toast Notification Fix

## Problem

Error in browser console:
```
TypeError: toast.success is not a function
TypeError: toast.error is not a function
```

## Root Cause

The `useToast()` composable was returning an object with separate properties:
```javascript
{
  toast: showToast,
  success: (msg) => ...,
  error: (msg) => ...,
  info: (msg) => ...
}
```

But the code was trying to call methods on the `toast` function:
```javascript
const { toast } = useToast();
toast.success('...');  // Error: toast is a function, not an object
toast.error('...');    // Error: toast is a function, not an object
```

## Solution

Modified the `useToast()` composable to return a `toast` function that also has `success`, `error`, and `info` methods attached to it:

### Before
```javascript
export function useToast() {
  return {
    toast: showToast,
    success: (msg, duration, options) => showToast(msg, 'success', duration, options),
    error: (msg, duration, options) => showToast(msg, 'error', duration, options),
    info: (msg, duration, options) => showToast(msg, 'info', duration, options),
  };
}
```

### After
```javascript
export function useToast() {
  const toast = (message, type = 'success', duration = 2500, options = {}) => {
    showToast(message, type, duration, options);
  };

  // Add methods to toast function
  toast.success = (msg, duration, options) => showToast(msg, 'success', duration, options);
  toast.error = (msg, duration, options) => showToast(msg, 'error', duration, options);
  toast.info = (msg, duration, options) => showToast(msg, 'info', duration, options);

  return {
    toast,
  };
}
```

## Benefits

1. **Backward compatible** - Existing code using `toast.success()`, `toast.error()`, etc. works without changes
2. **Flexible** - Can still use `toast('message')` for direct calls
3. **Clean** - No need to modify any Vue components
4. **Consistent** - Matches the usage pattern expected by the codebase

## Files Changed

1. `frontend/src/composables/useToast.js` - Modified the composable to attach methods to the toast function

## Verification

✅ Build successful
✅ No more TypeError in console
✅ All toast notifications work correctly

## Usage Examples

All of these now work:
```javascript
const { toast } = useToast();

// Method calls
toast.success('Success!');
toast.error('Error!');
toast.info('Info!');

// Direct call
toast('Custom message');
toast('Custom error', 'error', 5000);
```
