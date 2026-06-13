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
import { useState } from 'react'
import { Download, Plus, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { exportUsers } from '../api'
import { useUsers } from './users-provider'
import { UsersImportDialog } from './users-import-dialog'

export function UsersPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow, triggerRefresh } = useUsers()
  const [exportLoading, setExportLoading] = useState(false)
  const [importOpen, setImportOpen] = useState(false)

  const handleCreate = () => {
    setCurrentRow(null)
    setOpen('create')
  }

  const handleExport = async () => {
    setExportLoading(true)
    try {
      await exportUsers()
    } finally {
      setExportLoading(false)
    }
  }

  return (
    <>
      <div className='flex gap-2'>
        <Button variant='outline' size='sm' onClick={() => setImportOpen(true)}>
          <Upload className='h-4 w-4' />
          {t('Import')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          onClick={handleExport}
          disabled={exportLoading}
        >
          <Download className='h-4 w-4' />
          {exportLoading ? t('Exporting...') : t('Export')}
        </Button>
        <Button size='sm' onClick={handleCreate}>
          <Plus className='h-4 w-4' />
          {t('Add User')}
        </Button>
      </div>

      <UsersImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        onSuccess={triggerRefresh}
      />
    </>
  )
}
