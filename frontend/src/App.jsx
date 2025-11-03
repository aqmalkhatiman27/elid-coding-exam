import React, { useEffect, useState } from 'react'
const API = 'http://localhost:8080'

export default function App() {
  const [token, setToken] = useState('')
  const [devices, setDevices] = useState([])
  const [newDevice, setNewDevice] = useState({name:'', location:''})
  const [txns, setTxns] = useState([])
  const [poll, setPoll] = useState(false)

  const login = async () => {
    const res = await fetch(`${API}/auth/login`, {
      method:'POST', headers:{'Content-Type':'application/json'},
      body: JSON.stringify({email:'admin@elid.test', password:'admin123'})
    })
    const data = await res.json()
    setToken(data.token || '')
  }

  const headers = () => token ? { Authorization:`Bearer ${token}`, 'Content-Type':'application/json' } : {}

  const loadDevices = async () => {
    const res = await fetch(`${API}/api/devices`, { headers: headers() })
    setDevices(await res.json())
  }

  const createDevice = async (e) => {
    e.preventDefault()
    await fetch(`${API}/api/devices`, { method:'POST', headers: headers(), body: JSON.stringify(newDevice) })
    setNewDevice({name:'', location:''})
    loadDevices()
  }

  const toggle = async (id) => {
    await fetch(`${API}/api/devices/${id}/toggle`, { method:'POST', headers: headers() })
    loadDevices()
  }

  const activate = async (id) => {
    await fetch(`${API}/api/devices/${id}/activate`, { method:'POST', headers: headers() })
    setPoll(true)
  }

  const deactivate = async (id) => {
    await fetch(`${API}/api/devices/${id}/deactivate`, { method:'POST', headers: headers() })
  }

  const loadTxns = async () => {
    const res = await fetch(`${API}/api/transactions?limit=20`, { headers: headers() })
    setTxns(await res.json())
  }

  useEffect(() => { if (token) loadDevices() }, [token])
  useEffect(() => {
    if (!token || !poll) return
    loadTxns()
    const t = setInterval(loadTxns, 2000)
    return () => clearInterval(t)
  }, [token, poll])

  return (
    <div style={{maxWidth:900, margin:'2rem auto', fontFamily:'system-ui, sans-serif'}}>
      <h1>ELID Mini Console</h1>
      <p style={{opacity:.8}}>Demo: login → list/create devices → activate/deactivate → live transactions</p>

      <div style={{display:'flex', gap:12, alignItems:'center', marginTop:8}}>
        <button onClick={login} disabled={!!token}>{token ? 'Logged In' : 'Login (Demo)'}</button>
        {token && <button onClick={()=>{loadDevices(); loadTxns()}}>Refresh</button>}
      </div>

      <h2 style={{marginTop:'1.5rem'}}>Devices</h2>
      {!token && <p>Please click Login first.</p>}
      {token && (
        <>
          <form onSubmit={createDevice} style={{display:'flex', gap:8, marginBottom:12}}>
            <input placeholder="Name" value={newDevice.name} onChange={e=>setNewDevice({...newDevice, name:e.target.value})}/>
            <input placeholder="Location" value={newDevice.location} onChange={e=>setNewDevice({...newDevice, location:e.target.value})}/>
            <button type="submit">Add</button>
          </form>
          <table border="1" cellPadding="6" style={{borderCollapse:'collapse', width:'100%'}}>
            <thead>
              <tr><th>ID</th><th>Name</th><th>Location</th><th>State</th><th>Actions</th></tr>
            </thead>
            <tbody>
              {devices.map(d => (
                <tr key={d.id}>
                  <td>{d.id}</td>
                  <td>{d.name}</td>
                  <td>{d.location || '—'}</td>
                  <td>{d.is_locked ? 'LOCKED' : 'UNLOCKED'}</td>
                  <td style={{display:'flex', gap:8}}>
                    <button onClick={()=>toggle(d.id)}>Toggle</button>
                    <button onClick={()=>activate(d.id)}>Activate</button>
                    <button onClick={()=>deactivate(d.id)}>Deactivate</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}

      <h2 style={{marginTop:'1.5rem'}}>Recent Transactions {poll ? '(live)' : ''}</h2>
      {!token && <p>Login to view.</p>}
      {token && (
        <ul>
          {txns.map(t => (
            <li key={t.id}>
              #{t.id} • [{new Date(t.created_at).toLocaleTimeString()}] • {t.device} • {t.action}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
