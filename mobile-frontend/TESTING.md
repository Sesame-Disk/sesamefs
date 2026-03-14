# Mobile Frontend Testing Guide

## Quick Start
```bash
cd mobile-frontend
npm install
npm test              # Unit/integration tests
npm run test:coverage # With coverage report
npm run test:e2e      # Playwright E2E tests
```

## Dev Bypass Mode
Tests run without authentication using dev bypass mode:
- Set `PUBLIC_BYPASS_LOGIN=true` in `.env.development`
- Or set `BYPASS_LOGIN=true` env var in Docker
- This uses a dev token that skips login flow

## Running with Docker
```bash
# Build and run the full stack
docker-compose up --build

# Mobile frontend: http://localhost:4321
# Backend API: http://localhost:3000

# With bypass mode for testing:
BYPASS_LOGIN=true docker-compose up --build
```

## Test Types

### Unit Tests (Vitest + Testing Library)
- Location: `src/**/__tests__/*.test.{ts,tsx}`
- Run: `npm test`
- Coverage: `npm run test:coverage`
- Mock API: All API calls mocked, no real backend needed

### E2E Tests (Playwright)
- Location: `src/test/e2e/*.spec.ts`
- Run: `npm run test:e2e`
- UI mode: `npm run test:e2e:ui`
- Uses dev server with bypass mode
- Mobile viewport by default

## Target Devices

### Phones (must work perfectly)
- iPhone SE (320×568) - smallest supported
- iPhone 12/13/14 (390×844)
- iPhone 14 Pro Max (430×932)
- Samsung Galaxy S21 (360×800)
- Pixel 7 (412×915)

### Tablets (must work, split-view)
- iPad (768×1024)
- iPad Pro 11" (834×1194)
- Samsung Galaxy Tab (800×1280)

### Browsers
- Chrome Mobile (primary)
- Safari iOS (required for PWA)
- Firefox Mobile (secondary)
- Samsung Internet (secondary)

## Manual Test Checklist

### PWA
- [ ] Install prompt appears on Android Chrome
- [ ] Add to Home Screen works on iOS Safari
- [ ] App launches in standalone mode
- [ ] Manifest loaded correctly (DevTools > Application)
- [ ] Service worker registered
- [ ] Offline page shows when disconnected

### Navigation
- [ ] Bottom nav shows 5 tabs
- [ ] Active tab highlighted
- [ ] Tab switching works
- [ ] Back button works
- [ ] Deep links work (/libraries/{id}/path/to/folder/)

### Libraries
- [ ] Library list loads
- [ ] Pull-to-refresh works
- [ ] Sort options work (name, date, size)
- [ ] Create new library
- [ ] Navigate into library
- [ ] Encrypted library shows password dialog

### File Browser
- [ ] Directory contents load
- [ ] Breadcrumb navigation works
- [ ] List/grid toggle works
- [ ] File tap opens preview
- [ ] Folder tap navigates in
- [ ] Context menu (long press) works

### File Operations
- [ ] Rename file/folder
- [ ] Delete with confirmation
- [ ] Move to different folder
- [ ] Copy to different library
- [ ] Star/unstar files
- [ ] Multi-select mode
- [ ] Batch delete

### Upload
- [ ] Upload file from device
- [ ] Camera capture (mobile only)
- [ ] Upload progress shown
- [ ] Conflict handling (replace/skip/rename)
- [ ] New folder creation

### Preview
- [ ] Image: fullscreen with zoom
- [ ] Video: plays with controls
- [ ] Text: renders formatted
- [ ] PDF: embedded or download
- [ ] Unknown: download button

### Sharing
- [ ] Generate share link
- [ ] Copy link to clipboard
- [ ] QR code displayed
- [ ] Share via Web Share API
- [ ] Internal share with user picker

### Responsive
- [ ] No horizontal scroll at 320px
- [ ] Tablet shows split-view
- [ ] Landscape mode works
- [ ] Safe areas on notched phones
- [ ] All tap targets >= 44px

### Dark Mode
- [ ] System preference detected
- [ ] Manual toggle works
- [ ] All pages styled correctly
- [ ] Primary color visible in both modes

### Performance
- [ ] First load < 3s on 3G
- [ ] Navigation between pages < 500ms
- [ ] Smooth scrolling (60fps)
- [ ] No jank during animations
- [ ] Large file lists (100+ items) render smoothly
