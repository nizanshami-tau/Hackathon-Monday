import React, { useState, useEffect } from "react";
import { MultiSelect } from "react-multi-select-component";

const SelectGroups = () => {
    const [submitting, setSubmitting] = useState(false);
    const [selected, setSelected] = useState([]);
    const [groups, setGroups] = useState([]);
    
    const handleSubmit = async event => {
        const response = await fetch('https://sunday.sviry.net/gosvc/choosegroup', {
            method : 'POST',
            Headers :{
                'Content-Type': 'application/json'
            },
            body : selected
            });

            window.location.replace("/loading.html");
        
    }
    useEffect(() => {
       fetchData();
     }, []);
    const fetchData = async () => {
       let response = await (
         await fetch("https://sunday.sviry.net/groups2.json")
       ).json();
       setGroups(response);
     };

    return (
        <div>
            <h1>Select groups</h1>
            <pre>{JSON.stringify(selected)}</pre>
            <MultiSelect
                options={groups}
                value={selected}
                onChange={setSelected}
                labelledBy="Select"
            />

            <form onSubmit={handleSubmit}>
                <button type="submit">Submit</button>
            </form>
        </div>
        
    );
};

export default SelectGroups;