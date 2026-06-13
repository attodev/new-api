/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { importUsers } from '../api'
import type { ImportResult } from '../types'

interface UsersImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function UsersImportDialog({
  open,
  onOpenChange,
  onSuccess,
}: UsersImportDialogProps) {
  const { t } = useTranslation()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<ImportResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (f) {
      setFile(f)
      setResult(null)
      setError(null)
    }
  }

  const handleUpload = async () => {
    if (!file) return
    setLoading(true)
    setError(null)
    try {
      const res = await importUsers(file)
      setResult(res)
    } catch (e: any) {
      setError(e?.response?.data?.message ?? t('Import failed'))
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => {
    if (result) onSuccess()
    setFile(null)
    setResult(null)
    setError(null)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Import Users')}</DialogTitle>
        </DialogHeader>

        <div className='flex flex-col gap-4 py-2'>
          <input
            ref={fileInputRef}
            type='file'
            accept='.xlsx'
            className='hidden'
            onChange={handleFileChange}
          />

          <Button
            variant='outline'
            onClick={() => fileInputRef.current?.click()}
            disabled={loading}
          >
            {file ? file.name : t('Select .xlsx file')}
          </Button>

          {result && (
            <div className='rounded-md border p-3 text-sm space-y-1'>
              <p className='text-green-600'>
                ✓ {t('{{count}} users created', { count: result.created })}
              </p>
              {result.skipped > 0 && (
                <p className='text-amber-600'>
                  ⚠ {t('{{count}} users skipped (already exist)', { count: result.skipped })}: {result.skipped_usernames.join(', ')}
                </p>
              )}
              {result.errors.length > 0 && (
                <div className='text-red-600'>
                  <p>{t('Errors')}:</p>
                  <ul className='list-disc list-inside'>
                    {result.errors.map((e, i) => <li key={i}>{e}</li>)}
                  </ul>
                </div>
              )}
            </div>
          )}

          {error && (
            <p className='text-sm text-red-600'>{error}</p>
          )}
        </div>

        <DialogFooter className='gap-2'>
          {!result && (
            <Button
              onClick={handleUpload}
              disabled={!file || loading}
            >
              {loading ? t('Uploading...') : t('Upload')}
            </Button>
          )}
          <Button variant='outline' onClick={handleClose}>
            {result ? t('Close') : t('Cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
