import React, { useState, useEffect } from "react";
import { MultiSelect } from "react-multi-select-component";

const SelectGroups = () => {
    const [submitting, setSubmitting] = useState(false);
    const [selected, setSelected] = useState([]);
    const [groups, setGroups] = useState([]);
    
    const handleSubmit = event => {
        event.preventDefault();
       setSubmitting(true);
    
       setTimeout(() => {
         setSubmitting(false);
       }, 3000)
    }
    useEffect(() => {
       fetchData();
     }, []);
    const fetchData = async () => {
       let response = await (
         await fetch("https://sunday.sviry.net/gosvc/listgroups")
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