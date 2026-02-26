import { Toaster as SonnerToaster } from 'sonner'

export function Toaster() {
  return (
    <SonnerToaster
      position="bottom-right"
      toastOptions={{
        style: {
          background: '#191B22',
          border: '1px solid #2A2D38',
          color: '#EDEEF2',
          fontSize: '13px',
        },
      }}
      richColors
    />
  )
}
