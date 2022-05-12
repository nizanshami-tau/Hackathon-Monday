import React, { useState } from "react";
import { MultiSelect } from "react-multi-select-component";
import groupsJson from "./groups.json";


const groups = (json)=>{
    let i;
    let result = [];
    let arr = JSON.parse(json).groups;
    for(i = 0;i < arr.length;i++){
        result.push({ "label": arr[i].name, "value": arr[i].name})
    }

    return result;
}

const SelectGroups = () => {
    const [selected, setSelected] = useState([]);

    return (
        <div>
            <h1>Select groups</h1>
            <pre>{JSON.stringify(selected)}</pre>
            <MultiSelect
                options={ [{"label": "foo", "value": "bar"}] }
                value={selected}
                onChange={setSelected}
                labelledBy="Select"
            />
        </div>
    );
};

export default SelectGroups;