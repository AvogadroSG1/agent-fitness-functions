import { expect, test } from '@playwright/test'

test('loads the project overview without external resources', async ({ page }) => {
  const external: string[] = []
  page.on('request', request => { if (!request.url().startsWith(new URL(page.url()).origin)) external.push(request.url()) })
  await page.goto('/?repo=eshop-on-web')
  await expect(page.getByRole('heading', { name: 'Architecture browser' })).toBeVisible()
  await expect(page.getByText(/253 types/)).toBeVisible({ timeout: 30_000 })
  expect(external).toEqual([])
})

test('search reveals a type and detailed UML rows', async ({ page }) => {
  await page.goto('/?repo=eshop-on-web')
  const search = page.getByPlaceholder('Search types, members, projects')
  await search.fill('BasketItem')
  await page.getByRole('button', { name: /BasketItem ApplicationCore/ }).first().click()
  await expect(page.locator('.react-flow__node').filter({ hasText: 'BasketItem' }).first()).toBeVisible()
  await page.getByRole('combobox', { name: 'Detail' }).selectOption('detailed')
  await expect(page.getByText(/Declared members/)).toBeVisible()
})

test('browser never writes through graph interactions', async ({ page }) => {
  const writes: string[] = []
  page.on('request', request => { if (['POST', 'PUT', 'PATCH', 'DELETE'].includes(request.method())) writes.push(request.method()) })
  await page.goto('/?repo=eshop-on-web')
  await page.getByRole('button', { name: 'Re-layout' }).click()
  await page.getByRole('button', { name: 'Fit visible' }).click()
  expect(writes).toEqual([])
})
